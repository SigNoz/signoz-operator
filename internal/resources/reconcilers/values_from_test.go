package reconcilers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1/v1alpha1test"
	"github.com/SigNoz/signoz-operator/internal/jsonbody"
	"github.com/SigNoz/signoz-operator/internal/resources"
	"github.com/SigNoz/signoz-operator/internal/resources/resourcestest"
)

func TestWithValuesFrom(t *testing.T) {
	testCases := []struct {
		name          string
		valuesFrom    []resourcesv1alpha1.ValueFrom
		body          string
		expectedBody  string
		expectedError string
	}{
		{
			name: "NoValuesFrom_ObjectUnchanged",
			body: `{"name":"plain.example"}`,
		},
		{
			name: "SecretAndConfigMap_SetInBody",
			valuesFrom: []resourcesv1alpha1.ValueFrom{
				{Path: "config.spec.clientSecret", ValueFrom: resourcesv1alpha1.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth"}, Key: "client_secret"}}},
				{Path: "config.spec.clientId", ValueFrom: resourcesv1alpha1.ValueSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "oauth-public"}, Key: "client_id"}}},
			},
			body:         `{"name":"corp.example","config":{"kind":"google","spec":{"clientId":"","clientSecret":""}}}`,
			expectedBody: `{"name":"corp.example","config":{"kind":"google","spec":{"clientId":"id-from-configmap","clientSecret":"value-from-secret"}}}`,
		},
		{
			name: "SecretMissing_NotFound",
			valuesFrom: []resourcesv1alpha1.ValueFrom{
				{Path: "token", ValueFrom: resourcesv1alpha1.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "absent"}, Key: "token"}}},
			},
			expectedError: `spec.valuesFrom path "token": Secret "absent" not found in namespace "team"`,
		},
		{
			name: "ConfigMapKeyMissing_NotFound",
			valuesFrom: []resourcesv1alpha1.ValueFrom{
				{Path: "password", ValueFrom: resourcesv1alpha1.ValueSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "empty"}, Key: "nope"}}},
			},
			expectedError: `ConfigMap "empty" in namespace "team" has no key "nope"`,
		},
		{
			name: "ConfigMapMissing_NotFound",
			valuesFrom: []resourcesv1alpha1.ValueFrom{
				{Path: "issuer", ValueFrom: resourcesv1alpha1.ValueSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "gone"}, Key: "issuer"}}},
			},
			expectedError: `ConfigMap "gone" not found in namespace "team"`,
		},
		{
			name: "SecretInOtherNamespace_NotRead",
			valuesFrom: []resourcesv1alpha1.ValueFrom{
				{Path: "apiKey", ValueFrom: resourcesv1alpha1.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "foreign"}, Key: "api_key"}}},
			},
			expectedError: `Secret "foreign" not found`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{Name: "subject", Namespace: "team"},
				Spec:       resourcesv1alpha1.CoreSpec{ValuesFrom: testCase.valuesFrom},
			}

			reader := fake.NewClientBuilder().WithScheme(valuesFromScheme(t)).WithObjects(
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "oauth", Namespace: k8sObject.Namespace}, Data: map[string][]byte{"client_secret": []byte("value-from-secret")}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "oauth-public", Namespace: k8sObject.Namespace}, Data: map[string]string{"client_id": "id-from-configmap"}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: k8sObject.Namespace}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "elsewhere"}, Data: map[string][]byte{"api_key": []byte("k")}},
			).Build()

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().K8sObject().Return(k8sObject).Maybe()

			resolved, err := withValuesFrom(context.Background(), reader, obj)

			if testCase.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectedError)

				return
			}

			require.NoError(t, err)

			if testCase.expectedBody == "" {
				assert.Same(t, obj, resolved)
				return
			}

			obj.EXPECT().Body().Return(json.RawMessage(testCase.body), nil)
			obj.EXPECT().UpdatableFields().Return([]string{"config"})
			obj.EXPECT().ImmutableFields().Return([]string{"identifier"})

			body, err := resolved.Body()
			require.NoError(t, err)
			assert.JSONEq(t, testCase.expectedBody, string(body))

			hash, err := resolved.Hash()
			require.NoError(t, err)

			expectedHash, err := jsonbody.Hash(json.RawMessage(testCase.expectedBody))
			require.NoError(t, err)
			assert.Equal(t, expectedHash, hash)

			update, err := resolved.ToUpdate()
			require.NoError(t, err)
			assert.Contains(t, string(update), "value-from-secret")
			assert.NotContains(t, string(update), "corp.example")

			inSync, err := resolved.Compare(json.RawMessage(`{"data":` + testCase.expectedBody + `}`))
			require.NoError(t, err)
			assert.Empty(t, inSync.UpdatableFields)
			assert.Empty(t, inSync.ImmutableFields)

			drifted, err := resolved.Compare(json.RawMessage(`{"data":` + testCase.body + `}`))
			require.NoError(t, err)
			assert.Equal(t, []string{"config"}, drifted.UpdatableFields)

			// The custom resource itself never holds a resolved value.
			stored, err := json.Marshal(k8sObject)
			require.NoError(t, err)
			assert.NotContains(t, string(stored), "value-from-secret")
		})
	}
}

func TestValuesFromNotReady(t *testing.T) {
	reconciler, _, _ := newTestReconciler(t)
	reconciler.client = fake.NewClientBuilder().WithScheme(valuesFromScheme(t)).WithStatusSubresource(&v1alpha1test.FakeObject{}).Build()

	k8sObject := &v1alpha1test.FakeObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "waiting",
			Namespace:  "sso",
			Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
		},
		Spec: resourcesv1alpha1.CoreSpec{ValuesFrom: []resourcesv1alpha1.ValueFrom{
			{Path: "config.spec.clientSecret", ValueFrom: resourcesv1alpha1.ValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "not-yet-created"}, Key: "secret"}}},
		}},
	}
	require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

	obj := resourcestest.NewMockObject(t)
	obj.EXPECT().K8sObject().Return(k8sObject)
	obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
	obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
	obj.EXPECT().Identity().Return("sso.example", nil)

	result, err := reconciler.Reconcile(context.Background(), obj)
	require.NoError(t, err)
	assert.Equal(t, reconciler.defaultRetryInterval, result.RequeueAfter)

	fetched := &v1alpha1test.FakeObject{}
	require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))

	recoverable := meta.FindStatusCondition(fetched.Status.Conditions, resources.ConditionRecoverable.String())
	require.NotNil(t, recoverable)
	assert.Equal(t, metav1.ConditionTrue, recoverable.Status)
	assert.Equal(t, resources.ReasonValuesFromNotReady.String(), recoverable.Reason)
	assert.Contains(t, recoverable.Message, `Secret "not-yet-created" not found in namespace "sso"`)
}

func valuesFromScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := v1alpha1test.Scheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	return scheme
}
