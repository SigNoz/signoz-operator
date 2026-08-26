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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

func TestInvalidSpec(t *testing.T) {
	testCases := []struct {
		name            string
		identityErr     error
		hashErr         error
		expectedMessage string
	}{
		{
			name:            "IdentityFails_Terminal",
			identityErr:     errors.New("spec carries no name"),
			expectedMessage: "could not derive identity: spec carries no name",
		},
		{
			name:            "HashFails_Terminal",
			hashErr:         errors.New("jsonSpec is not renderable"),
			expectedMessage: "could not calculate hash: jsonSpec is not renderable",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, _, _ := newTestReconciler(t)

			// The finalizer is already present so addFinalizer does not hit the client.
			// The object must exist in the client for the status update at the end of
			// Reconcile to succeed.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "invalid-spec",
					Namespace:  "invalid-spec-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("", testCase.identityErr)

			// The hash is only rendered once an identity could be derived.
			if testCase.identityErr == nil {
				obj.EXPECT().Hash().Return("", testCase.hashErr)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)

			// A malformed spec settles the object: no requeue of any kind.
			assert.Equal(t, ctrl.Result{}, result)

			// Assert on the status read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &v1alpha1test.FakeObject{}
			require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched))

			terminal := meta.FindStatusCondition(fetched.Status.Conditions, resources.ConditionTerminal.String())
			if assert.NotNil(t, terminal) {
				assert.Equal(t, metav1.ConditionTrue, terminal.Status)
				assert.Equal(t, resources.ReasonInvalidSpec.String(), terminal.Reason)
				assert.Equal(t, testCase.expectedMessage, terminal.Message)
			}
			assert.True(t, meta.IsStatusConditionFalse(fetched.Status.Conditions, resources.ConditionReady.String()))  // Ready=False
			assert.True(t, meta.IsStatusConditionFalse(fetched.Status.Conditions, resources.ConditionSynced.String())) // Synced=False

			// The status changed, so the pass must be stamped and persisted.
			assert.False(t, fetched.Status.ReconciledAt.IsZero())
		})
	}
}

func TestClientWriteFails(t *testing.T) {
	testCases := []struct {
		name             string
		hasFinalizer     bool
		suspend          bool
		hasCreateAttempt bool
		recordedID       string
		expectsFind      bool
		findIDs          []string
	}{
		{
			name: "AddFinalizerFails_ReturnsError",
		},
		{
			name:         "StatusUpdateFails_ReturnsError",
			hasFinalizer: true,
			suspend:      true,
		},
		{
			name:             "RemoveCreateAttemptAnnotationFails_ReturnsError",
			hasFinalizer:     true,
			hasCreateAttempt: true,
			recordedID:       "durable-id",
		},
		{
			name:         "AddCreateAttemptAnnotationOnCreateFails_ReturnsError",
			hasFinalizer: true,
			expectsFind:  true,
		},
		{
			name:         "AddCreateAttemptAnnotationOnAdoptFails_ReturnsError",
			hasFinalizer: true,
			expectsFind:  true,
			findIDs:      []string{"adoptable-id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			// The object is deliberately never created in the client, so every write
			// the reconciler attempts against it fails.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "client-write",
					Namespace: "client-write-ns",
				},
				Spec: resourcesv1alpha1.CoreSpec{Suspend: testCase.suspend},
			}
			if testCase.hasFinalizer {
				k8sObject.Finalizers = []string{resourcesv1alpha1.ResourceFinalizer}
			}

			if testCase.hasCreateAttempt {
				k8sObject.Annotations = map[string]string{resourcesv1alpha1.AnnotationCreateAttempt: "2026-01-01T00:00:00Z"}
			}

			if testCase.recordedID != "" {
				k8sObject.Status.SigNozResource = &resourcesv1alpha1.SigNozResource{ID: &testCase.recordedID}
			}

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)

			// Nothing past addFinalizer runs when it is the write that fails.
			if testCase.hasFinalizer {
				obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			}

			if testCase.hasFinalizer && !testCase.suspend {
				obj.EXPECT().Identity().Return("client-write-identity", nil)
				obj.EXPECT().Hash().Return("client-write-hash", nil)
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
			}

			if testCase.expectsFind {
				candidates := make([]*resourcesv1alpha1.SigNozResource, 0, len(testCase.findIDs))
				for _, id := range testCase.findIDs {
					candidates = append(candidates, &resourcesv1alpha1.SigNozResource{ID: &id})
				}

				adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(candidates, nil)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.True(t, apierrors.IsNotFound(err))
			assert.Equal(t, ctrl.Result{}, result)
		})
	}
}

func TestExistingObject(t *testing.T) {
	remote := json.RawMessage(`{"title":"remote"}`)
	currentHash := "existing-object-hash"

	testCases := []struct {
		name                  string
		hasCreateAttempt      bool
		observedHash          string
		readErr               error
		compareResult         resources.CompareResult
		compareErr            error
		updateErr             error
		expectedUpdate        bool
		expectedRequeueAfter  time.Duration
		expectedReason        resources.Reason
		expectedReady         metav1.ConditionStatus
		expectedSynced        metav1.ConditionStatus
		expectedTrueCondition string // Terminal or Recoverable; empty means neither may be present
		expectedObservedHash  string
		expectedIDDropped     bool
	}{
		{
			name:                 "RemoteNotFound_DropsMetadataAndRequeues",
			hasCreateAttempt:     true,
			observedHash:         "gone-hash",
			readErr:              internalerrors.NewFromHTTPResponse(http.StatusNotFound, nil),
			expectedRequeueAfter: 1 * time.Second, // requeueAfterOnNotFound
			expectedReason:       resources.ReasonPending,
			expectedReady:        metav1.ConditionUnknown,
			expectedSynced:       metav1.ConditionUnknown,
			expectedIDDropped:    true,
		},
		{
			name:                  "ReadFails_Recoverable",
			hasCreateAttempt:      true,
			observedHash:          "unread-hash",
			readErr:               internalerrors.NewFromHTTPResponse(http.StatusInternalServerError, nil),
			expectedRequeueAfter:  3 * time.Second, // spec.retryInterval
			expectedReason:        resources.ReasonBackendError,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
			expectedObservedHash:  "unread-hash",
		},
		{
			name:                  "CompareFails_Terminal",
			hasCreateAttempt:      true,
			compareErr:            errors.New("unexpected end of JSON input"),
			expectedReason:        resources.ReasonCompareFailed,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                  "ImmutableFieldChanged_Terminal",
			hasCreateAttempt:      true,
			compareResult:         resources.CompareResult{ImmutableFields: []string{"name"}},
			expectedReason:        resources.ReasonImmutableFieldChanged,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                 "DriftByCompare_Updates",
			hasCreateAttempt:     true,
			compareResult:        resources.CompareResult{UpdatableFields: []string{"description"}},
			expectedUpdate:       true,
			expectedRequeueAfter: 2 * time.Second, // defaultInterval
			expectedReason:       resources.ReasonUpdated,
			expectedReady:        metav1.ConditionTrue,
			expectedSynced:       metav1.ConditionTrue,
			expectedObservedHash: currentHash,
		},
		{
			// Compare sees nothing, so only the hash can report the drift: a field
			// the operator does not map changed in the spec.
			name:                 "DriftByHashOnly_Updates",
			hasCreateAttempt:     true,
			observedHash:         "outdated-hash",
			expectedUpdate:       true,
			expectedRequeueAfter: 2 * time.Second, // defaultInterval
			expectedReason:       resources.ReasonUpdated,
			expectedReady:        metav1.ConditionTrue,
			expectedSynced:       metav1.ConditionTrue,
			expectedObservedHash: currentHash,
		},
		{
			name:                 "NoDrift_Synced",
			expectedRequeueAfter: 2 * time.Second, // defaultInterval
			expectedReason:       resources.ReasonSynced,
			expectedReady:        metav1.ConditionTrue,
			expectedSynced:       metav1.ConditionTrue,
			expectedObservedHash: currentHash,
		},
		{
			name:                  "UpdateFails_Recoverable",
			hasCreateAttempt:      true,
			observedHash:          "unapplied-hash",
			compareResult:         resources.CompareResult{UpdatableFields: []string{"description"}},
			updateErr:             internalerrors.NewFromHTTPResponse(http.StatusTooManyRequests, nil),
			expectedUpdate:        true,
			expectedRequeueAfter:  3 * time.Second, // spec.retryInterval
			expectedReason:        resources.ReasonBackendError,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
			expectedObservedHash:  "unapplied-hash",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			// spec.retryInterval is set so a recoverable requeue is distinguishable
			// from both the steady-state interval and requeueAfterOnNotFound.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "existing-object",
					Namespace:  "existing-object-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
				Spec: resourcesv1alpha1.CoreSpec{RetryInterval: &metav1.Duration{Duration: 3 * time.Second}},
			}
			if testCase.hasCreateAttempt {
				k8sObject.Annotations = map[string]string{resourcesv1alpha1.AnnotationCreateAttempt: "2026-02-02T00:00:00Z"}
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			// The id goes through the status writer so the client holds it too, not
			// just the in-memory object the mock hands back.
			recordedID := "recorded-id"
			k8sObject.Status.SigNozResource = &resourcesv1alpha1.SigNozResource{ID: &recordedID}
			k8sObject.Status.ObservedHash = testCase.observedHash
			require.NoError(t, reconciler.client.Status().Update(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("existing-identity", nil)
			obj.EXPECT().Hash().Return(currentHash, nil)

			resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)

			if testCase.readErr != nil {
				adapter.EXPECT().Read(mock.Anything, mock.Anything, obj, mock.Anything).Return(nil, testCase.readErr)
			} else {
				adapter.EXPECT().Read(mock.Anything, mock.Anything, obj, mock.Anything).Return(remote, nil)
				obj.EXPECT().Compare(remote).Return(testCase.compareResult, testCase.compareErr)
			}

			if len(testCase.compareResult.ImmutableFields) > 0 {
				obj.EXPECT().ImmutableFields().Return([]string{"name", "kind"})
			}

			if testCase.expectedUpdate {
				adapter.EXPECT().Update(mock.Anything, mock.Anything, obj, mock.Anything).Return(testCase.updateErr)
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

			assert.Equal(t, testCase.expectedObservedHash, fetched.Status.ObservedHash)

			if testCase.expectedIDDropped {
				assert.Nil(t, fetched.Status.SigNozResource)
			} else {
				id, idErr := resourcesv1alpha1.GetIDFromSigNozResource(fetched.Status.SigNozResource)
				require.NoError(t, idErr)
				assert.Equal(t, recordedID, id)
			}

			// The id is durably in status, so the create-attempt annotation has
			// served its purpose and must be gone.
			assert.NotContains(t, fetched.Annotations, resourcesv1alpha1.AnnotationCreateAttempt)

			// The status changed, so the pass must be stamped and persisted.
			assert.False(t, fetched.Status.ReconciledAt.IsZero())
		})
	}
}

func TestCreateFails(t *testing.T) {
	testCases := []struct {
		name                  string
		createErr             error
		expectedRequeueAfter  time.Duration
		expectedReason        resources.Reason
		expectedReady         metav1.ConditionStatus
		expectedSynced        metav1.ConditionStatus
		expectedTrueCondition string
		expectedCreateAttempt bool
	}{
		{
			name:                  "NotRetryable_DropsCreateAttemptAnnotation",
			createErr:             internalerrors.NewFromHTTPResponse(http.StatusBadRequest, json.RawMessage(`{"error":"title is required"}`)),
			expectedReason:        resources.ReasonRejected,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                  "Retryable_KeepsCreateAttemptAnnotation",
			createErr:             internalerrors.NewFromHTTPResponse(http.StatusServiceUnavailable, nil),
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonBackendError,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
			expectedCreateAttempt: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "create-fails",
					Namespace:  "create-fails-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("create-fails-identity", nil)
			obj.EXPECT().Hash().Return("create-fails-hash", nil)

			resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
			adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(nil, nil)
			adapter.EXPECT().Create(mock.Anything, mock.Anything, obj).Return(nil, testCase.createErr)

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
			assert.True(t, meta.IsStatusConditionTrue(fetched.Status.Conditions, testCase.expectedTrueCondition))

			assert.Nil(t, fetched.Status.SigNozResource)
			assert.Empty(t, fetched.Status.ObservedHash)

			// A create that may still have landed keeps the annotation; one the
			// server refused outright cannot have, so it is dropped.
			_, hasCreateAttempt := fetched.Annotations[resourcesv1alpha1.AnnotationCreateAttempt]
			assert.Equal(t, testCase.expectedCreateAttempt, hasCreateAttempt)
		})
	}
}

func TestCreateAttemptAnnotationRemovalFails(t *testing.T) {
	t.Parallel()

	reconciler, resolver, adapter := newTestReconciler(t)

	k8sObject := &v1alpha1test.FakeObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "annotation-removal",
			Namespace:  "annotation-removal-ns",
			Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
		},
	}

	// create patches the annotation on before issuing the create and off again
	// once the server refuses it, so failing only the second patch puts the
	// removal under test.
	patches := 0
	reconciler.client = fake.NewClientBuilder().
		WithScheme(v1alpha1test.Scheme()).
		WithStatusSubresource(&v1alpha1test.FakeObject{}).
		WithObjects(k8sObject).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, kubeClient client.WithWatch, patched client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patches++

				if patches == 2 {
					return apierrors.NewInternalError(errors.New("etcdserver: request timed out"))
				}

				return kubeClient.Patch(ctx, patched, patch, opts...)
			},
		}).
		Build()

	obj := resourcestest.NewMockObject(t)
	obj.EXPECT().K8sObject().Return(k8sObject)
	obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
	obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
	obj.EXPECT().Identity().Return("annotation-removal-identity", nil)
	obj.EXPECT().Hash().Return("annotation-removal-hash", nil)

	resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
	adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(nil, nil)
	adapter.EXPECT().Create(mock.Anything, mock.Anything, obj).Return(nil, internalerrors.NewFromHTTPResponse(http.StatusBadRequest, nil))

	result, err := reconciler.Reconcile(context.Background(), obj)
	assert.True(t, apierrors.IsInternalError(err))
	assert.Equal(t, ctrl.Result{}, result)
}

func TestConflict(t *testing.T) {
	remote := json.RawMessage(`{"title":"conflicting"}`)

	testCases := []struct {
		name                  string
		candidateIDs          []string
		findErr               error
		expectedAdopt         bool
		expectedRequeueAfter  time.Duration
		expectedReason        resources.Reason
		expectedReady         metav1.ConditionStatus
		expectedSynced        metav1.ConditionStatus
		expectedTrueCondition string // Terminal or Recoverable; empty means neither may be present
		expectedID            string
	}{
		{
			name:                 "OneCandidate_Adopts",
			candidateIDs:         []string{"conflicting-id"},
			expectedAdopt:        true,
			expectedRequeueAfter: 2 * time.Second, // defaultInterval
			expectedReason:       resources.ReasonSynced,
			expectedReady:        metav1.ConditionTrue,
			expectedSynced:       metav1.ConditionTrue,
			expectedID:           "conflicting-id",
		},
		{
			// SigNoz refused the create but nothing is findable yet, so the write
			// may still surface: retry rather than settle.
			name:                  "NoCandidates_Recoverable",
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonBackendError,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
		},
		{
			name:                  "ManyCandidates_Terminal",
			candidateIDs:          []string{"first-conflicting-id", "second-conflicting-id"},
			expectedReason:        resources.ReasonAmbiguous,
			expectedReady:         metav1.ConditionFalse,
			expectedSynced:        metav1.ConditionFalse,
			expectedTrueCondition: resources.ConditionTerminal.String(),
		},
		{
			name:                  "FindFails_Recoverable",
			findErr:               errors.New("dial tcp: i/o timeout"),
			expectedRequeueAfter:  1 * time.Second, // defaultRetryInterval
			expectedReason:        resources.ReasonBackendUnreachable,
			expectedReady:         metav1.ConditionUnknown,
			expectedSynced:        metav1.ConditionUnknown,
			expectedTrueCondition: resources.ConditionRecoverable.String(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "conflict",
					Namespace:  "conflict-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("conflict-identity", nil)
			obj.EXPECT().Hash().Return("conflict-hash", nil)

			resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)

			candidates := make([]*resourcesv1alpha1.SigNozResource, 0, len(testCase.candidateIDs))
			for _, id := range testCase.candidateIDs {
				candidates = append(candidates, &resourcesv1alpha1.SigNozResource{ID: &id})
			}

			// The first lookup sees nothing, so a create is issued; SigNoz then
			// reports the conflict the second lookup has to resolve.
			adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(nil, nil).Once()
			adapter.EXPECT().Create(mock.Anything, mock.Anything, obj).Return(nil, internalerrors.NewFromHTTPResponse(http.StatusConflict, nil))
			adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(candidates, testCase.findErr).Once()

			if testCase.expectedAdopt {
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
			} else {
				assert.Nil(t, fetched.Status.SigNozResource)
			}

			// A conflict never clears the annotation: an object bound to this
			// resource may exist in SigNoz whatever the lookup said.
			assert.Contains(t, fetched.Annotations, resourcesv1alpha1.AnnotationCreateAttempt)
		})
	}
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		name                 string
		finalizers           []string
		reclaimPolicy        resourcesv1alpha1.ReclaimPolicy
		recordedID           string
		readsStatus          bool
		expectsReclaim       bool
		deleteErr            error
		expectedRequeueAfter time.Duration
		expectedFinalizers   []string // nil means the object must be gone
	}{
		{
			name:               "ForeignFinalizerOnly_NoOp",
			finalizers:         []string{"example.com/keeper"},
			reclaimPolicy:      resourcesv1alpha1.ReclaimDelete,
			expectedFinalizers: []string{"example.com/keeper"},
		},
		{
			name:          "OrphanPolicy_RemovesFinalizer",
			finalizers:    []string{resourcesv1alpha1.ResourceFinalizer},
			reclaimPolicy: resourcesv1alpha1.ReclaimOrphan,
			recordedID:    "orphaned-id",
		},
		{
			name:           "RecordedID_ReclaimsAndRemovesFinalizer",
			finalizers:     []string{resourcesv1alpha1.ResourceFinalizer},
			reclaimPolicy:  resourcesv1alpha1.ReclaimDelete,
			recordedID:     "reclaimed-id",
			readsStatus:    true,
			expectsReclaim: true,
		},
		{
			// Already gone in SigNoz is the outcome the reclaim wanted.
			name:           "DeleteNotFound_RemovesFinalizer",
			finalizers:     []string{resourcesv1alpha1.ResourceFinalizer},
			reclaimPolicy:  resourcesv1alpha1.ReclaimDelete,
			recordedID:     "vanished-id",
			readsStatus:    true,
			expectsReclaim: true,
			deleteErr:      internalerrors.NewFromHTTPResponse(http.StatusNotFound, nil),
		},
		{
			name:                 "DeleteFails_RequeuesAndKeepsFinalizer",
			finalizers:           []string{resourcesv1alpha1.ResourceFinalizer},
			reclaimPolicy:        resourcesv1alpha1.ReclaimDelete,
			recordedID:           "undeletable-id",
			readsStatus:          true,
			expectsReclaim:       true,
			deleteErr:            internalerrors.NewFromHTTPResponse(http.StatusInternalServerError, nil),
			expectedRequeueAfter: 1 * time.Second, // defaultRetryInterval
			expectedFinalizers:   []string{resourcesv1alpha1.ResourceFinalizer},
		},
		{
			// Nothing was ever bound, so deletion need not wait on a provider config.
			name:          "NeverBound_RemovesFinalizerWithoutResolving",
			finalizers:    []string{resourcesv1alpha1.ResourceFinalizer},
			reclaimPolicy: resourcesv1alpha1.ReclaimDelete,
			readsStatus:   true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "finalize",
					Namespace:  "finalize-ns",
					Finalizers: testCase.finalizers,
				},
				Spec: resourcesv1alpha1.CoreSpec{ReclaimPolicy: testCase.reclaimPolicy},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			if testCase.recordedID != "" {
				k8sObject.Status.SigNozResource = &resourcesv1alpha1.SigNozResource{ID: &testCase.recordedID}
				require.NoError(t, reconciler.client.Status().Update(context.Background(), k8sObject))
			}

			// Deleting an object that carries a finalizer only stamps the deletion
			// timestamp, which is what drives Reconcile into finalize.
			require.NoError(t, reconciler.client.Delete(context.Background(), k8sObject))
			require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject))
			require.False(t, k8sObject.GetDeletionTimestamp().IsZero())

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)

			if testCase.readsStatus {
				obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			}

			if testCase.expectsReclaim {
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
				adapter.EXPECT().Delete(mock.Anything, mock.Anything, obj, k8sObject.Status.SigNozResource).Return(testCase.deleteErr)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: testCase.expectedRequeueAfter}, result)

			// Assert on the state read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &v1alpha1test.FakeObject{}
			getErr := reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched)

			if testCase.expectedFinalizers == nil {
				// Dropping the last finalizer lets the pending deletion complete.
				assert.True(t, apierrors.IsNotFound(getErr))
				return
			}

			require.NoError(t, getErr)
			assert.Equal(t, testCase.expectedFinalizers, fetched.Finalizers)
		})
	}
}

func TestFinalizeUnconfirmedCreate(t *testing.T) {
	testCases := []struct {
		name                 string
		resolveErr           error
		identityErr          error
		foundIDs             []string
		findErr              error
		expectsReclaim       bool
		expectedRequeueAfter time.Duration
		expectedFinalizers   []string // nil means the object must be gone
	}{
		{
			name:                 "ProviderConfigUnresolvable_RequeuesAndKeepsFinalizer",
			resolveErr:           errors.New("provider config is not ready"),
			expectedRequeueAfter: 1 * time.Second, // defaultRetryInterval
			expectedFinalizers:   []string{resourcesv1alpha1.ResourceFinalizer},
		},
		{
			// Without an identity there is nothing left to look the object up by.
			name:        "IdentityFails_RemovesFinalizer",
			identityErr: errors.New("spec carries no name"),
		},
		{
			name:                 "FindFails_RequeuesAndKeepsFinalizer",
			findErr:              errors.New("dial tcp: connection refused"),
			expectedRequeueAfter: 1 * time.Second, // defaultRetryInterval
			expectedFinalizers:   []string{resourcesv1alpha1.ResourceFinalizer},
		},
		{
			name: "NoMatch_RemovesFinalizer",
		},
		{
			name:           "OneMatch_ReclaimsAndRemovesFinalizer",
			foundIDs:       []string{"unconfirmed-id"},
			expectsReclaim: true,
		},
		{
			// Deleting the wrong object is worse than waiting, so never guess.
			name:                 "ManyMatches_RequeuesAndKeepsFinalizer",
			foundIDs:             []string{"first-unconfirmed-id", "second-unconfirmed-id"},
			expectedRequeueAfter: 1 * time.Second, // defaultRetryInterval
			expectedFinalizers:   []string{resourcesv1alpha1.ResourceFinalizer},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)

			// No id was ever recorded, but the create-attempt annotation says an
			// object may have been left behind in SigNoz.
			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "unconfirmed-create",
					Namespace:   "unconfirmed-create-ns",
					Finalizers:  []string{resourcesv1alpha1.ResourceFinalizer},
					Annotations: map[string]string{resourcesv1alpha1.AnnotationCreateAttempt: "2026-03-03T00:00:00Z"},
				},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))
			require.NoError(t, reconciler.client.Delete(context.Background(), k8sObject))
			require.NoError(t, reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)

			if testCase.resolveErr != nil {
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, testCase.resolveErr)
			} else {
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
				obj.EXPECT().Identity().Return("unconfirmed-identity", testCase.identityErr)
			}

			if testCase.resolveErr == nil && testCase.identityErr == nil {
				found := make([]*resourcesv1alpha1.SigNozResource, 0, len(testCase.foundIDs))
				for _, id := range testCase.foundIDs {
					found = append(found, &resourcesv1alpha1.SigNozResource{ID: &id})
				}

				adapter.EXPECT().Find(mock.Anything, mock.Anything, obj).Return(found, testCase.findErr)
			}

			if testCase.expectsReclaim {
				adapter.EXPECT().Delete(mock.Anything, mock.Anything, obj, mock.Anything).Return(nil)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: testCase.expectedRequeueAfter}, result)

			// Assert on the state read back from the client, not the in-memory one,
			// so a path that forgets to persist fails here.
			fetched := &v1alpha1test.FakeObject{}
			getErr := reconciler.client.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), fetched)

			if testCase.expectedFinalizers == nil {
				// Dropping the last finalizer lets the pending deletion complete.
				assert.True(t, apierrors.IsNotFound(getErr))
				return
			}

			require.NoError(t, getErr)
			assert.Equal(t, testCase.expectedFinalizers, fetched.Finalizers)
		})
	}
}

func TestIntervals(t *testing.T) {
	testCases := []struct {
		name                 string
		specInterval         *metav1.Duration
		specRetryInterval    *metav1.Duration
		defaultRetryInterval time.Duration
		resolveErr           error
		expectedRequeueAfter time.Duration
	}{
		{
			name:                 "SpecInterval_UsedWhenSynced",
			specInterval:         &metav1.Duration{Duration: 9 * time.Second},
			defaultRetryInterval: 1 * time.Second,
			expectedRequeueAfter: 9 * time.Second,
		},
		{
			name:                 "SpecRetryInterval_WinsOverSpecInterval",
			specInterval:         &metav1.Duration{Duration: 9 * time.Second},
			specRetryInterval:    &metav1.Duration{Duration: 7 * time.Second},
			defaultRetryInterval: 1 * time.Second,
			resolveErr:           errors.New("secret has no such key"),
			expectedRequeueAfter: 7 * time.Second,
		},
		{
			name:                 "SpecRetryIntervalUnset_FallsBackToSpecInterval",
			specInterval:         &metav1.Duration{Duration: 4 * time.Second},
			defaultRetryInterval: 1 * time.Second,
			resolveErr:           errors.New("secret could not be read"),
			expectedRequeueAfter: 4 * time.Second,
		},
		{
			name:                 "SpecUnset_FallsBackToDefaultRetryInterval",
			defaultRetryInterval: 6 * time.Second,
			resolveErr:           errors.New("provider config could not be read"),
			expectedRequeueAfter: 6 * time.Second,
		},
		{
			name:                 "DefaultRetryIntervalUnset_FallsBackToDefaultInterval",
			resolveErr:           errors.New("provider config is being deleted"),
			expectedRequeueAfter: 2 * time.Second, // defaultInterval
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reconciler, resolver, adapter := newTestReconciler(t)
			reconciler.defaultRetryInterval = testCase.defaultRetryInterval

			k8sObject := &v1alpha1test.FakeObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "intervals",
					Namespace:  "intervals-ns",
					Finalizers: []string{resourcesv1alpha1.ResourceFinalizer},
				},
				Spec: resourcesv1alpha1.CoreSpec{
					Interval:      testCase.specInterval,
					RetryInterval: testCase.specRetryInterval,
				},
			}
			require.NoError(t, reconciler.client.Create(context.Background(), k8sObject))

			obj := resourcestest.NewMockObject(t)
			obj.EXPECT().K8sObject().Return(k8sObject)
			obj.EXPECT().GetCoreSpec().Return(&k8sObject.Spec)
			obj.EXPECT().GetCoreStatus().Return(&k8sObject.Status)
			obj.EXPECT().Identity().Return("intervals-identity", nil)
			obj.EXPECT().Hash().Return("intervals-hash", nil)

			if testCase.resolveErr != nil {
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, testCase.resolveErr)
			} else {
				// An object already in sync is the only outcome that requeues at the
				// steady-state interval.
				recordedID := "intervals-id"
				k8sObject.Status.SigNozResource = &resourcesv1alpha1.SigNozResource{ID: &recordedID}
				require.NoError(t, reconciler.client.Status().Update(context.Background(), k8sObject))

				remote := json.RawMessage(`{"title":"intervals"}`)
				resolver.EXPECT().ResolveRef(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&providerconfig.ResolvedProviderConfigSpec{}, nil)
				adapter.EXPECT().Read(mock.Anything, mock.Anything, obj, mock.Anything).Return(remote, nil)
				obj.EXPECT().Compare(remote).Return(resources.CompareResult{}, nil)
			}

			result, err := reconciler.Reconcile(context.Background(), obj)
			assert.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: testCase.expectedRequeueAfter}, result)
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
