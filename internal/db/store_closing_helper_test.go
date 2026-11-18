package db

import "errors"

// isStoreClosing reports whether err is the refusal a write gets once the store is closing.
//
// Plan 0009 phase P1 wrote the tests that use it before ErrStoreClosing existed, and this helper then matched the
// refusal by its text so that they compiled and failed for the right reason; phase P2 made it the sentinel test.
func isStoreClosing(err error) bool {
	return errors.Is(err, ErrStoreClosing)
}
