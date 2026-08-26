package reconcilers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	internalerrors "github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/providerconfig/providerconfigtest"
)

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name                   string
		generation             int64
		versions               map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string
		resolveErr             error
		expectedReady          metav1.ConditionStatus
		expectedReason         string
		expectedFailureMessage string
		expectedRefVersions    map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string
	}{
		{
			name:           "ResolveSucceeds_ReadyTrue",
			generation:     3,
			expectedReady:  metav1.ConditionTrue,
			expectedReason: providerconfig.ReasonResolved.String(),
		},
		{
			name:       "ResolveSucceeds_VersionsRecordedPerKind",
			generation: 4,
			versions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {
					{Namespace: "billing", Name: "signoz-credential"}: "141",
				},
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {
					{Namespace: "billing", Name: "signoz-endpoint"}: "142",
				},
			},
			expectedReady:  metav1.ConditionTrue,
			expectedReason: providerconfig.ReasonResolved.String(),
			expectedRefVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret:    {"billing/signoz-credential": "141"},
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {"billing/signoz-endpoint": "142"},
			},
		},
		{
			name:       "ResolveSucceeds_KindWithoutRefsOmitted",
			generation: 5,
			versions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {
					{Namespace: "ledger", Name: "api-token"}: "77",
				},
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {},
			},
			expectedReady:  metav1.ConditionTrue,
			expectedReason: providerconfig.ReasonResolved.String(),
			expectedRefVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {"ledger/api-token": "77"},
			},
		},
		{
			// The resolver reports what it read before it failed, so a failure
			// still records versions.
			name:       "ResolveFails_SecretNotFound_ReasonFromCode",
			generation: 6,
			versions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {
					{Namespace: "payments", Name: "signoz-ca"}: "8",
				},
			},
			resolveErr:             internalerrors.New(internalerrors.ReasonNotFound, `secret "signoz-credential" not found`).WithCode(providerconfig.CodeSecretNotFound),
			expectedReady:          metav1.ConditionFalse,
			expectedReason:         providerconfig.CodeSecretNotFound.String(),
			expectedFailureMessage: `secret "signoz-credential" not found`,
			expectedRefVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {"payments/signoz-ca": "8"},
			},
		},
		{
			name:                   "ResolveFails_EndpointInvalid_ReasonFromCode",
			generation:             7,
			resolveErr:             internalerrors.New(internalerrors.ReasonInvalidInput, "endpoint is not an absolute URL").WithCode(providerconfig.CodeEndpointInvalid),
			expectedReady:          metav1.ConditionFalse,
			expectedReason:         providerconfig.CodeEndpointInvalid.String(),
			expectedFailureMessage: "endpoint is not an absolute URL",
		},
		{
			// Message drops the wrapped cause the Error string carries.
			name:                   "ResolveFails_UncodedBaseError_ReasonReferenceReadFailed",
			generation:             8,
			resolveErr:             internalerrors.Wrap(errors.New("etcdserver: leader changed"), internalerrors.ReasonInternal, "could not read ConfigMap"),
			expectedReady:          metav1.ConditionFalse,
			expectedReason:         providerconfig.CodeReferenceReadFailed.String(),
			expectedFailureMessage: "could not read ConfigMap",
		},
		{
			name:                   "ResolveFails_PlainError_ReasonReferenceReadFailed",
			generation:             9,
			resolveErr:             errors.New("dial tcp: connection refused"),
			expectedReady:          metav1.ConditionFalse,
			expectedReason:         providerconfig.CodeReferenceReadFailed.String(),
			expectedFailureMessage: "dial tcp: connection refused",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &resourcesv1alpha1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "tenant-backend",
					Namespace:  "tenant",
					Generation: testCase.generation,
				},
				Spec: newTestSpec("https://signoz.tenant.svc:8080"),
			}

			kubeClient := newTestClient(t, config)

			resolver := providerconfigtest.NewMockResolver(t)
			resolver.EXPECT().Resolve(mock.Anything, config.Namespace, &config.Spec).Return(nil, testCase.versions, testCase.resolveErr)

			result, err := NewCommonReconciler(kubeClient, resolver).Reconcile(context.Background(), config, &config.Spec, &config.Status, config.Namespace)
			assert.Equal(t, ctrl.Result{}, result)

			if testCase.resolveErr != nil {
				assert.ErrorIs(t, err, testCase.resolveErr)
			} else {
				assert.NoError(t, err)
			}

			// Assert on the state read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &resourcesv1alpha1.ProviderConfig{}
			require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(config), fetched))

			// A resolved spec always reports the same message; only a failure
			// carries one of its own.
			expectedMessage := "Endpoint and credential resolved"
			if testCase.resolveErr != nil {
				expectedMessage = testCase.expectedFailureMessage
			}

			ready := meta.FindStatusCondition(fetched.Status.Conditions, providerconfig.ConditionReady)
			require.NotNil(t, ready)
			assert.Equal(t, testCase.expectedReady, ready.Status)
			assert.Equal(t, testCase.expectedReason, ready.Reason)
			assert.Equal(t, expectedMessage, ready.Message)
			assert.Equal(t, testCase.generation, ready.ObservedGeneration)

			assert.Equal(t, testCase.generation, fetched.Status.ObservedGeneration)
			assert.Equal(t, testCase.expectedRefVersions, fetched.Status.ObservedRefVersions)
		})
	}
}

func TestReconcileClusterProviderConfig(t *testing.T) {
	testCases := []struct {
		name         string
		refNamespace string
	}{
		{
			name:         "OperatorNamespace_PassedThrough",
			refNamespace: "signoz-operator",
		},
		{
			name:         "EmptyNamespace_PassedThrough",
			refNamespace: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &resourcesv1alpha1.ClusterProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "shared-backend",
					Generation: 2,
				},
				Spec: newTestSpec("https://signoz.shared.svc:8080"),
			}

			kubeClient := newTestClient(t, config)

			resolver := providerconfigtest.NewMockResolver(t)
			resolver.EXPECT().Resolve(mock.Anything, testCase.refNamespace, &config.Spec).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil, nil)

			result, err := NewCommonReconciler(kubeClient, resolver).Reconcile(context.Background(), config, &config.Spec, &config.Status, testCase.refNamespace)
			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			fetched := &resourcesv1alpha1.ClusterProviderConfig{}
			require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(config), fetched))

			assert.True(t, meta.IsStatusConditionTrue(fetched.Status.Conditions, providerconfig.ConditionReady))
			assert.Equal(t, int64(2), fetched.Status.ObservedGeneration)
		})
	}
}

func TestReconcileTwice(t *testing.T) {
	testCases := []struct {
		name                string
		firstVersions       map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string
		secondVersions      map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string
		expectedUpdates     int
		expectedRefVersions map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string
	}{
		{
			name: "SameOutcome_SkipsTheUpdate",
			firstVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {
					{Namespace: "ops", Name: "signoz-key"}: "12",
				},
			},
			secondVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {
					{Namespace: "ops", Name: "signoz-key"}: "12",
				},
			},
			expectedUpdates: 1,
			expectedRefVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[string]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindSecret: {"ops/signoz-key": "12"},
			},
		},
		{
			name: "RefsGone_ClearsObservedRefVersions",
			firstVersions: map[resourcesv1alpha1.ProviderConfigObservedRefKind]map[client.ObjectKey]string{
				resourcesv1alpha1.ProviderConfigObservedRefKindConfigMap: {
					{Namespace: "infra", Name: "signoz-url"}: "31",
				},
			},
			expectedUpdates: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &resourcesv1alpha1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "steady-backend",
					Namespace:  "steady",
					Generation: 1,
				},
				Spec: newTestSpec("https://signoz.steady.svc:8080"),
			}

			updates := 0
			kubeClient := fake.NewClientBuilder().
				WithScheme(newTestScheme(t)).
				WithStatusSubresource(config).
				WithObjects(config).
				WithInterceptorFuncs(interceptor.Funcs{
					SubResourceUpdate: func(ctx context.Context, inner client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						updates++

						return inner.SubResource(subResourceName).Update(ctx, obj, opts...)
					},
				}).
				Build()

			resolver := providerconfigtest.NewMockResolver(t)
			resolver.EXPECT().Resolve(mock.Anything, config.Namespace, &config.Spec).Return(nil, testCase.firstVersions, nil).Once()
			resolver.EXPECT().Resolve(mock.Anything, config.Namespace, &config.Spec).Return(nil, testCase.secondVersions, nil).Once()

			reconciler := NewCommonReconciler(kubeClient, resolver)

			_, err := reconciler.Reconcile(context.Background(), config, &config.Spec, &config.Status, config.Namespace)
			require.NoError(t, err)

			_, err = reconciler.Reconcile(context.Background(), config, &config.Spec, &config.Status, config.Namespace)
			require.NoError(t, err)

			assert.Equal(t, testCase.expectedUpdates, updates)

			fetched := &resourcesv1alpha1.ProviderConfig{}
			require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(config), fetched))

			assert.Equal(t, testCase.expectedRefVersions, fetched.Status.ObservedRefVersions)
			assert.True(t, meta.IsStatusConditionTrue(fetched.Status.Conditions, providerconfig.ConditionReady))
		})
	}
}

func TestReconcileStatusUpdateFails(t *testing.T) {
	t.Parallel()

	config := &resourcesv1alpha1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "unwritable-backend",
			Namespace:  "unwritable",
			Generation: 1,
		},
		Spec: newTestSpec("https://signoz.unwritable.svc:8080"),
	}

	updateErr := apierrors.NewInternalError(errors.New("etcdserver: request timed out"))
	kubeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(config).
		WithObjects(config).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
				return updateErr
			},
		}).
		Build()

	resolver := providerconfigtest.NewMockResolver(t)
	resolver.EXPECT().Resolve(mock.Anything, config.Namespace, &config.Spec).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil, nil)

	result, err := NewCommonReconciler(kubeClient, resolver).Reconcile(context.Background(), config, &config.Spec, &config.Status, config.Namespace)
	assert.Equal(t, ctrl.Result{}, result)
	assert.ErrorContains(t, err, "could not update status")
	assert.ErrorIs(t, err, updateErr)
}

// A spec with content, so an argument matcher on it rejects a fresh one.
func newTestSpec(endpoint string) resourcesv1alpha1.ProviderConfigSpec {
	return resourcesv1alpha1.ProviderConfigSpec{
		Endpoint: resourcesv1alpha1.Endpoint{Value: endpoint},
		Auth: resourcesv1alpha1.Authentication{
			Header: &resourcesv1alpha1.HeaderAuth{Name: providerconfig.DefaultHeaderName, Value: "signoz-api-key"},
		},
	}
}

func newTestClient(t *testing.T, config client.Object) client.Client {
	t.Helper()

	return fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(config).
		WithObjects(config).
		Build()
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, resourcesv1alpha1.AddToScheme(scheme))

	return scheme
}
