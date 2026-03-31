package auth

// User represents an authenticated user making a request.
// It is carried through the document lifecycle via DocContext.
type User struct {
	// Email is the user's unique email address and login identifier.
	Email string
	// FullName is the user's display name.
	FullName string
	// Roles is the list of role names assigned to this user.
	Roles []string
}
