package constcond

import (
	. "unsafe"

	"go.temporal.io/sdk/workflow"
)

// unsafe.Sizeof through a dot import: still machine dependent.
func DotImportedUnsafe(ctx workflow.Context) error {
	if Sizeof(uintptr(0)) == 8 {
		return step(ctx)
	}
	return nil
}
