// Unexported helpers exposed to the black-box test package.
//
// Both carry a rule that is invisible from outside the package but wrong in a
// way no integration test would catch cheaply: one refuses inputs that would
// silently corrupt a group's membership, the other decides whether a failed
// transaction is retried. They are tested directly rather than through the
// live paths that use them, which need infrastructure and could only exercise
// the happy branch.
//
// No build tag: this belongs to the ordinary unit-test binary, unlike
// export_test.go, which is experimental apparatus for the integration run.

package firestore

var (
	// DedupeExcluding is dedupeExcluding.
	DedupeExcluding = dedupeExcluding

	// IsOptionsMismatch is isOptionsMismatch.
	IsOptionsMismatch = isOptionsMismatch
)
