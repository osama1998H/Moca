package core

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/moca-framework/moca/pkg/document"
)

// UserController provides custom lifecycle logic for User documents.
// It embeds BaseController for no-op defaults on all other lifecycle events.
type UserController struct {
	document.BaseController
}

// BeforeSave hashes the password field with bcrypt if it has been modified
// and is not already a bcrypt hash (prefixed with $2a$ or $2b$).
func (c *UserController) BeforeSave(_ *document.DocContext, doc document.Document) error {
	modified := doc.ModifiedFields()
	passwordModified := false
	for _, f := range modified {
		if f == "password" {
			passwordModified = true
			break
		}
	}
	if !passwordModified {
		return nil
	}

	raw, ok := doc.Get("password").(string)
	if !ok || raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "$2a$") || strings.HasPrefix(raw, "$2b$") {
		return nil
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: hash password: %w", err)
	}

	return doc.Set("password", string(hashed))
}

// NewUserController is the factory function for UserController.
// It satisfies the document.DocLifecycleFactory type signature.
func NewUserController() document.DocLifecycle {
	return &UserController{}
}
