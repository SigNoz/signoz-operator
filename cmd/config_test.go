package main

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func TestBuildCacheOptions(t *testing.T) {
	testCases := []struct {
		name                string
		watchNamespaces     []string
		referenceNamespaces []string
		operatorNamespace   string
		watched             int
		referenced          int
	}{
		{
			name:              "NoNamespaces_WatchesEverything",
			operatorNamespace: "signoz",
		},
		{
			name:              "WatchNamespaces_ReferencesFollowWatched",
			watchNamespaces:   []string{"team-a", " team-b "},
			operatorNamespace: "platform",
			watched:           3,
		},
		{
			name:                "ReferenceNamespaces_ResourcesWatchedEverywhere",
			referenceNamespaces: []string{"payments"},
			operatorNamespace:   "operators",
			referenced:          2,
		},
		{
			name:                "ReferenceNamespaces_OperatorNamespaceNotRepeated",
			referenceNamespaces: []string{"observability"},
			operatorNamespace:   "observability",
			referenced:          1,
		},
		{
			name:                "WatchAndReferenceNamespaces_ScopedApart",
			watchNamespaces:     []string{"checkout", "search"},
			referenceNamespaces: []string{"vault"},
			operatorNamespace:   "infra",
			watched:             3,
			referenced:          2,
		},
		{
			name:                "BlankNamespaces_Ignored",
			watchNamespaces:     []string{" "},
			referenceNamespaces: []string{""},
			operatorNamespace:   "monitoring",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := &config{
				WatchNamespaces:     testCase.watchNamespaces,
				ReferenceNamespaces: testCase.referenceNamespaces,
				OperatorNamespace:   testCase.operatorNamespace,
			}

			options := cfg.buildCacheOptions()

			assert.Len(t, options.DefaultNamespaces, testCase.watched)

			if testCase.watched > 0 {
				assert.Subset(t, slices.Collect(maps.Keys(options.DefaultNamespaces)), namespacesIn(testCase.watchNamespaces, testCase.operatorNamespace))
			}

			if testCase.referenced == 0 {
				assert.Empty(t, options.ByObject)
				return
			}

			byType := map[reflect.Type]cache.ByObject{}
			for obj, byObject := range options.ByObject {
				byType[reflect.TypeOf(obj)] = byObject
			}

			require.Len(t, byType, 2)

			for _, objectType := range []reflect.Type{reflect.TypeFor[*corev1.Secret](), reflect.TypeFor[*corev1.ConfigMap]()} {
				require.Contains(t, byType, objectType)
				assert.Len(t, byType[objectType].Namespaces, testCase.referenced)
				assert.Subset(t, slices.Collect(maps.Keys(byType[objectType].Namespaces)), namespacesIn(testCase.referenceNamespaces, testCase.operatorNamespace))
			}
		})
	}
}

func namespacesIn(namespaces []string, operatorNamespace string) []string {
	trimmed := make([]string, 0, len(namespaces)+1)
	trimmed = append(trimmed, operatorNamespace)

	for _, ns := range namespaces {
		trimmed = append(trimmed, strings.TrimSpace(ns))
	}

	return trimmed
}
