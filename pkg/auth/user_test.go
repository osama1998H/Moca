package auth

import "testing"

func TestUser_ZeroValue(t *testing.T) {
	var u User
	if u.Email != "" {
		t.Errorf("zero-value Email = %q, want empty", u.Email)
	}
	if u.FullName != "" {
		t.Errorf("zero-value FullName = %q, want empty", u.FullName)
	}
	if u.Roles != nil {
		t.Errorf("zero-value Roles = %v, want nil", u.Roles)
	}
	if u.UserDefaults != nil {
		t.Errorf("zero-value UserDefaults = %v, want nil", u.UserDefaults)
	}
}

func TestUser_FieldAssignment(t *testing.T) {
	u := User{
		Email:    "admin@example.com",
		FullName: "Admin User",
		Roles:    []string{"Administrator", "Sales User"},
		UserDefaults: map[string]string{
			"company":   "ACME Corp",
			"territory": "West",
		},
	}

	if u.Email != "admin@example.com" {
		t.Errorf("Email = %q", u.Email)
	}
	if u.FullName != "Admin User" {
		t.Errorf("FullName = %q", u.FullName)
	}
	if len(u.Roles) != 2 {
		t.Errorf("Roles len = %d, want 2", len(u.Roles))
	}
	if u.Roles[0] != "Administrator" || u.Roles[1] != "Sales User" {
		t.Errorf("Roles = %v", u.Roles)
	}
	if u.UserDefaults["company"] != "ACME Corp" {
		t.Errorf("UserDefaults[company] = %q", u.UserDefaults["company"])
	}
	if u.UserDefaults["territory"] != "West" {
		t.Errorf("UserDefaults[territory] = %q", u.UserDefaults["territory"])
	}
}

func TestUser_EmptyRoles(t *testing.T) {
	u := User{
		Email: "guest@example.com",
		Roles: []string{},
	}
	if len(u.Roles) != 0 {
		t.Errorf("Roles len = %d, want 0", len(u.Roles))
	}
}

func TestUser_NilUserDefaults(t *testing.T) {
	u := User{
		Email:        "user@example.com",
		UserDefaults: nil,
	}
	// Accessing nil map should return zero value.
	if v := u.UserDefaults["company"]; v != "" {
		t.Errorf("nil UserDefaults[company] = %q, want empty", v)
	}
}
