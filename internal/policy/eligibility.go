package policy

// Eligibility explains whether an issue may be dispatched.
type Eligibility struct {
	Allowed bool
	Reason  string
}
