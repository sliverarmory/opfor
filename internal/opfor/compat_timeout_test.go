package opfor

import (
	"runtime"
	"time"
)

// compatibilityExecutionTimeout is a deadlock sentinel, not a throughput
// requirement. The canonical CPU/fork/I/O stress fixtures take substantially
// longer on Windows CI runners, so keep their execution bounded while allowing
// for that platform's observed scheduling and I/O overhead.
func compatibilityExecutionTimeout(base time.Duration) time.Duration {
	if runtime.GOOS == "windows" {
		return 4 * base
	}
	return base
}
