package instrumentation

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func NewLoggerWithZap(lvl string) (logr.Logger, error) {
	level, err := zapcore.ParseLevel(lvl)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("level %q is not a valid level: %w", lvl, err)
	}

	return zap.New(zap.UseDevMode(false), zap.Level(level)), nil
}

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
