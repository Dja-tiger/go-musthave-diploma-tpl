package auth

import "testing"

func TestManagerIssueVerify(t *testing.T) {
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	token := manager.Issue(42)
	userID, ok := manager.Verify(token)
	if !ok {
		t.Fatal("token must verify")
	}
	if userID != 42 {
		t.Fatalf("userID = %d, want 42", userID)
	}
}

func TestManagerRejectsTamperedToken(t *testing.T) {
	manager, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	token := manager.Issue(42)
	if _, ok := manager.Verify("43" + token[2:]); ok {
		t.Fatal("tampered token must not verify")
	}
}
