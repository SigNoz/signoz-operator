package instrumentation

import (
	"context"
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LoggerFromContext names the logger after the calling function, receiver type
// included, derived from the call stack rather than hand-typed.
func LoggerFromContext(ctx context.Context) logr.Logger {
	logger := logf.FromContext(ctx)

	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return logger
	}

	// github.com/.../reconcilers.(*commonReconciler).reconcile → commonReconciler.reconcile
	name := runtime.FuncForPC(pc).Name()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}

	if i := strings.Index(name, "."); i >= 0 {
		name = name[i+1:]
	}

	name = strings.NewReplacer("(", "", "*", "", ")", "").Replace(name)

	return logger.WithName(name)
}
