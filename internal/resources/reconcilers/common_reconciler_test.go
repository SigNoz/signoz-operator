package reconcilers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1/v1alpha1test"
	internalerrors "github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/providerconfig"
	"github.com/SigNoz/signoz-operator/internal/providerconfig/providerconfigtest"
	"github.com/SigNoz/signoz-operator/internal/resources"
	"github.com/SigNoz/signoz-operator/internal/resources/resourcestest"
)

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

			reconciler, resolver, _ := newTestReconciler(t)
			reconciler.defaultTimeout = testCase.defaultTimeout

			// The finalizer is already present so addFinalizer does not hit the client.
			// The object must exist in the client for the status update at the end of
			// Reconcile to succeed.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "timeout",
					Namespace:  "timeout-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
				Spec: resourcesv1alpha1.CoreSpec{Timeout: testCase.specTimeout},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
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

			// Assert on the status read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &v1alpha1test.FakeObject{}
			require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))

			recoverable := meta.FindStatusCondition(fetched.Status.Conditions, resources.ConditionRecoverable.String())
			if assert.NotNil(t, recoverable) {
				assert.Equal(t, metav1.ConditionTrue, recoverable.Status)
				assert.Equal(t, resources.ReasonProviderConfigNotReady.String(), recoverable.Reason)
				assert.Contains(t, recoverable.Message, testCase.expectedMessage)
			}
			assert.True(t, meta.IsStatusConditionPresentAndEqual(fetched.Status.Conditions, resources.ConditionReady.String(), metav1.ConditionUnknown))  // Ready=Unknown
			assert.True(t, meta.IsStatusConditionPresentAndEqual(fetched.Status.Conditions, resources.ConditionSynced.String(), metav1.ConditionUnknown)) // Synced=Unknown

			// The status changed, so the pass must be stamped and persisted.
			assert.False(t, fetched.Status.ReconciledAt.IsZero())

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

	reconciler, _, _ := newTestReconciler(t)

	// The object starts without the finalizer and must exist in the client for
	// addFinalizer's Update to succeed.
	k8sObject := &v1alpha1test.FakeObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "suspend",
			Namespace: "suspend-ns",
		},
		Spec: resourcesv1alpha1.CoreSpec{Suspend: true},
	}
	require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

	obj := resourcestest.NewMockObject(t)
	obj.EXPECT().K8sObject().Return(k8sObject)
	obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
	obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)

	result, err := reconciler.Reconcile(context.Background(), obj)
	assert.NoError(t, err)

	// Suspend settles the object: no requeue of any kind.
	assert.Equal(t, ctrl.Result{}, result)

	// Assert on the status read back from the client, not the in-memory one,
	// so a path that forgets to persist fails here.
	fetched := &v1alpha1test.FakeObject{}
	require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))

	suspended := meta.FindStatusCondition(fetched.Status.Conditions, resources.ConditionSuspended.String())
	if assert.NotNil(t, suspended) {
		assert.Equal(t, metav1.ConditionTrue, suspended.Status) // Suspended=True
		assert.Equal(t, resources.ReasonSuspended.String(), suspended.Reason)
	}
	assert.True(t, meta.IsStatusConditionFalse(fetched.Status.Conditions, resources.ConditionReady.String()))                                     // Ready=False
	assert.True(t, meta.IsStatusConditionPresentAndEqual(fetched.Status.Conditions, resources.ConditionSynced.String(), metav1.ConditionUnknown)) // Synced=Unknown

	// The status changed, so the pass must be stamped and persisted.
	assert.False(t, fetched.Status.ReconciledAt.IsZero())

	// The finalizer must be persisted, not just set on the in-memory object.
	assert.Contains(t, fetched.Finalizers, resourcesv1alpha1.ResourceFinalizer)
}

func TestNewObject(t *testing.T) {
	pin := "pinned-id"

	testCases := []struct {
		name                  string
		pinnedID              string
		candidateIDs          []string
		findErr               error
		expectedCreate        bool
		expectedAdopt         bool
		expectedRequeueAfter  time.Duration
		expectedReason        resources.Reason
		expectedReady         metav1.ConditionStatus
		expectedSynced        metav1.ConditionStatus
		expectedTrueCondition string // Terminal or Recoverable; empty means neither may be present
		expectedID            string // id persisted in status; empty means none
		expectedCreateAttempt bool
	}{
		{
			name:                  "NoCandidates_Creates",
			expectedCreate:        true,
			expectedRequeueAfter:  2 * time.Second, // defaultInterval
			expectedReason:        resources.ReasonCreated,
			expectedReady:         metav1.ConditionTrue,
			expectedSynced:        metav1.ConditionTrue,
			expectedID:            "created-id",
			expectedCreateAttempt: true,
		},
		{
			name:                  "OneCandidate_Adopts",
			candidateIDs:          []string{"existing-id"},
			expectedAdopt:         true,
			expectedRequeueAfter:  2 * time.Second, // defaultInterval
			expectedReason:        resources.ReasonSynced,
			expectedReady:         metav1.ConditionTrue,
			expectedSynced:        metav1.ConditionTrue,
			expectedID:            "existing-id",
			expectedCreateAttempt: true,
		},
		{
			name:                  "ManyCandidates_Terminal",
			candidateIDs:          []string{"first-id", "second-id"},
			expectedReason:        resources.ReasonAmbiguous,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                  "PinnedIDAmongCandidates_AdoptsPinned",
			pinnedID:              pin,
			candidateIDs:          []string{"other-id", pin},
			expectedAdopt:         true,
			expectedRequeueAfter:  2 * time.Second, // defaultInterval
			expectedReason:        resources.ReasonSynced,
			expectedReady:         metav1.ConditionTrue,
			expectedSynced:        metav1.ConditionTrue,
			expectedID:            pin,
			expectedCreateAttempt: true,
		},
		{
			name:                  "PinnedIDMatchesNothing_Terminal",
			pinnedID:              "missing-id",
			candidateIDs:          []string{"only-id"},
			expectedReason:        resources.ReasonSigNozResourceIDMismatch,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                  "FindFails_Recoverable",
			findErr:               errors.New("dial tcp: connection refused"),
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonBackendUnreachable,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
		},
		{
			// The credential can be fixed without touching this resource, so a
			// rejected one must keep retrying rather than settle on Terminal.
			name:                  "FindUnauthorized_Recoverable",
			findErr:               internalerrors.NewFromHTTPResponse(http.StatusUnauthorized, nil),
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonUnauthorized,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
		},
		{
			// A missing role on the service account the key belongs to, which is
			// granted in SigNoz and so moves nothing here for a watch to see.
			name:                  "FindForbidden_Recoverable",
			findErr:               internalerrors.NewFromHTTPResponse(http.StatusForbidden, nil),
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonUnauthorized,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			// The finalizer is already present so addFinalizer does not hit the client.
			// The object must exist in the client for the create-attempt annotation
			// patch and the status update at the end of Reconcile to succeed.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "new-object",
					Namespace:  "new-object-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
			}
			if testCase.pinnedID != "" {
				k8sObject.Annotations = map[string]string{resourcesv1alpha1.AnnotationSigNozResourceID: testCase.pinnedID}
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("identity", nil)
			obj.EXPECT().Hash().Return("hash", nil)

			resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)

			candidates := make([]*resourcesv1alpha1.SigNozResource, 0, len(testCase.candidateIDs))
			for _, id := range testCase.candidateIDs {
				candidates = append(candidates, &resourcesv1alpha1.SigNozResource{ID: &id})
			}

			adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(candidates, testCase.findErr)

			if testCase.expectedCreate {
				createdID := testCase.expectedID
				adapter.EXPECT().Create(mock.Anything, mock.Anything, obj).Return(&resourcesv1alpha1.SigNozResource{ID: &createdID}, nil)
			}

			if testCase.expectedAdopt {
				remote := json.RawMessage(`{}`)
				adapter.EXPECT().Read(mock.Anything, mock.Anything, obj, mock.Anything).Return(remote, nil)
				obj.EXPECT().Compare(remote).Return(resources.CompareResult{}, nil)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: testCase.expectedRequeueAfter}, result)

			// Assert on the state read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &v1alpha1test.FakeObject{}
			require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))

			synced := meta.FindStatusCondition(fetched.Status.Conditions, resources.ConditionSynced.String())
			if assert.NotNil(t, synced) {
				assert.Equal(t, testCase.expectedSynced, synced.Status)
				assert.Equal(t, testCase.expectedReason.String(), synced.Reason)
			}
			assert.True(t, meta.IsStatusConditionPresentAndEqual(fetched.Status.Conditions, resources.ConditionReady.String(), testCase.expectedReady))

			for _, conditionType := range []string{resources.ConditionTerminal.String(), resources.ConditionRecoverable.String()} {
				if conditionType == testCase.expectedTrueCondition {
					assert.True(t, meta.IsStatusConditionTrue(fetched.Status.Conditions, conditionType))
				} else {
					assert.Nil(t, meta.FindStatusCondition(fetched.Status.Conditions, conditionType))
				}
			}

			if testCase.expectedID != "" {
				id, idErr := resourcesv1alpha1.GetIDFromSigNozResource(fetched.Status.SigNozResource)
				require.NoError(t, idErr)
				assert.Equal(t, testCase.expectedID, id)
				assert.Equal(t, "hash", fetched.Status.ObservedHash)
			} else {
				assert.Nil(t, fetched.Status.SigNozResource)
				assert.Empty(t, fetched.Status.ObservedHash)
			}

			// A bound object (created or adopted) must carry the create-attempt
			// annotation until a later pass sees the id durably in status.
			_, hasCreateAttempt := fetched.Annotations[resourcesv1alpha1.AnnotationCreateAttempt]
			assert.Equal(t, testCase.expectedCreateAttempt, hasCreateAttempt)

			// The status changed, so the pass must be stamped and persisted.
			assert.False(t, fetched.Status.ReconciledAt.IsZero())
		})
	}
}

func newTestReconciler(t *testing.T) (*commonReconciler, *providerconfigtest.MockResolver, *resourcestest.MockAdapter) {
	resolver := providerconfigtest.NewMockResolver(t)
	adapter := resourcestest.NewMockAdapter(t)
	testReconciler := NewCommonReconciler(
		// Reconcile persists status changes, so the fake client must know FakeObject
		// and treat it as having a status subresource.
		fake.NewClientBuilder().WithScheme(v1alpha1test.Scheme()).WithStatusSubresource(&v1alpha1test.FakeObject{}).Build(),
		resolver,
		adapter,
		2*time.Second, // interval
		1*time.Second, // retryInterval
		5*time.Second, // timeout
		"operator",
	)

	return testReconciler.(*commonReconciler), resolver, adapter
}
