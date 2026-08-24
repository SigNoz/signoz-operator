package reconcilers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/providerconfig/providerconfigtest"
	"github.com/SigNoz/signoz-operator/internal/resources"
	"github.com/SigNoz/signoz-operator/internal/resources/resourcestest"
)

func newTestReconciler(t *testing.T) (*commonReconciler, *providerconfigtest.MockResolver) {
	resolver := providerconfigtest.NewMockResolver(t)
	testReconciler := NewCommonReconciler(
		fake.NewClientBuilder().Build(),
		resolver,
		resourcestest.NewMockAdapter(t),
		2*time.Second, // interval
		1*time.Second, // retryInterval
		5*time.Second, // timeout
		"operator",
	)

	return testReconciler.(*commonReconciler), resolver
}

func TestTimeout(t *testing.T) {
	testCases := []struct {
		name            string
		defaultTimeout  time.Duration
		specTimeout     *metav1.Duration
		expectedTimeout time.Duration // 0 means the context must carry no deadline
		expectedMessage string
	}{
		{
			name:            "SpecTimeoutNil_DefaultApplied",
			defaultTimeout:  500 * time.Millisecond,
			specTimeout:     nil,
			expectedTimeout: 500 * time.Millisecond,
			expectedMessage: context.DeadlineExceeded.Error(),
		},
		{
			name:            "SpecTimeoutSet_OverridesDefault",
			defaultTimeout:  500 * time.Millisecond,
			specTimeout:     &metav1.Duration{Duration: 200 * time.Millisecond},
			expectedTimeout: 200 * time.Millisecond,
			expectedMessage: context.DeadlineExceeded.Error(),
		},
		{
			name:            "SpecTimeoutZero_DefaultApplied",
			defaultTimeout:  500 * time.Millisecond,
			specTimeout:     &metav1.Duration{Duration: 0},
			expectedTimeout: 500 * time.Millisecond,
			expectedMessage: context.DeadlineExceeded.Error(),
		},
		{
			name:            "SpecTimeoutNil_NoDefault_NoDeadline",
			defaultTimeout:  0,
			specTimeout:     nil,
			expectedTimeout: 0,
			expectedMessage: "provider config is not ready",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver := newTestReconciler(t)
			reconciler.defaultTimeout = testCase.defaultTimeout

			// The finalizer is already present so addFinalizer does not hit the client.
			k8sObject := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "default",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
			}

			status := &resourcesv1alpha1.CoreStatus{}
			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&resourcesv1alpha1.CoreSpec{Timeout: testCase.specTimeout})
			obj.EXPECT().GetCoreStatus().Return(status)
			obj.EXPECT().Identity().Return("identity", nil)
			obj.EXPECT().Hash().Return("hash", nil)

			// ResolveRef outlives the deadline Reconcile derived, so its context
			// error must surface as the condition message. Without a deadline it
			// would block forever, so it fails immediately instead.
			var deadline time.Time
			var hasDeadline bool
			resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, _ resourcesv1alpha1.ProviderConfigRef, _, _ string) (*providerconfig.ResolvedProviderConfigSpec, error) {
				deadline, hasDeadline = ctx.Deadline()

				if !hasDeadline {
					return nil, errors.New("provider config is not ready")
				}

				<-ctx.Done()

				return nil, ctx.Err()
			})

			start := time.Now()

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)
			assert.Equal(t, reconciler.defaultRetryInterval, result.RequeueAfter)

			recoverable := meta.FindStatusCondition(status.Conditions, resources.ConditionRecoverable.String())
			if assert.NotNil(t, recoverable) {
				assert.Equal(t, metav1.ConditionTrue, recoverable.Status)
				assert.Equal(t, resources.ReasonProviderConfigNotReady.String(), recoverable.Reason)
				assert.Contains(t, recoverable.Message, testCase.expectedMessage)
			}
			assert.True(t, meta.IsStatusConditionPresentAndEqual(status.Conditions, resources.ConditionReady.String(), metav1.ConditionUnknown))  // Ready=Unknown
			assert.True(t, meta.IsStatusConditionPresentAndEqual(status.Conditions, resources.ConditionSynced.String(), metav1.ConditionUnknown)) // Synced=Unknown

			if testCase.expectedTimeout == 0 {
				assert.False(t, hasDeadline)
				return
			}

			assert.True(t, hasDeadline)

			// timeout <= deadline - start < timeout + 100ms (to catch that the correct timeout was selected for SpecTimeoutSet_OverridesDefault (200ms and 500ms) -> it needs to be less than 300ms and more than 150-60ms for scheduling delays)
			assert.GreaterOrEqual(t, deadline.Sub(start), testCase.expectedTimeout)
			assert.Less(t, deadline.Sub(start), testCase.expectedTimeout+100*time.Millisecond)
		})
	}
}

func TestAddFinalizerAndSuspend(t *testing.T) {
	t.Parallel()

	reconciler, _ := newTestReconciler(t)

	// The object starts without the finalizer and must exist in the client for
	// addFinalizer's Update to succeed.
	k8sObject := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

	status := &resourcesv1alpha1.CoreStatus{}
	obj := resourcestest.NewMockObject(t)
	obj.EXPECT().K8sObject().Return(k8sObject)
	obj.EXPECT().GetCoreSpec().Return(&resourcesv1alpha1.CoreSpec{Suspend: true})
	obj.EXPECT().GetCoreStatus().Return(status)

	result, err := reconciler.Reconcile(context.Background(), obj)
	assert.NoError(t, err)

	// Suspend settles the object: no requeue of any kind.
	assert.Equal(t, ctrl.Result{}, result)

	suspended := meta.FindStatusCondition(status.Conditions, resources.ConditionSuspended.String())
	if assert.NotNil(t, suspended) {
		assert.Equal(t, metav1.ConditionTrue, suspended.Status) // Suspended=True
		assert.Equal(t, resources.ReasonSuspended.String(), suspended.Reason)
	}
	assert.True(t, meta.IsStatusConditionFalse(status.Conditions, resources.ConditionReady.String()))                                     // Ready=False
	assert.True(t, meta.IsStatusConditionPresentAndEqual(status.Conditions, resources.ConditionSynced.String(), metav1.ConditionUnknown)) // Synced=Unknown

	// The finalizer must be persisted, not just set on the in-memory object.
	fetched := &corev1.ConfigMap{}
	require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))
	assert.Contains(t, fetched.Finalizers, resourcesv1alpha1.ResourceFinalizer)
}
