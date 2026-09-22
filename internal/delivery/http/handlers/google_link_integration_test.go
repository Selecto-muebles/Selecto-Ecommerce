package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestGoogleLinkCommitsAndRetriesWithoutDuplicateAudit(t *testing.T) {
	f := newGoogleLinkFixture(t)
	for i := 0; i < 2; i++ {
		response := f.perform(f.email, "user", "google-subject")
		if response.Code != http.StatusOK || response.Body.String() != `{"linked":true}` {
			t.Fatalf("attempt %d: status=%d body=%s", i, response.Code, response.Body.String())
		}
	}
	f.assertState(t, 1, 1, true)
}

func TestGoogleLinkPreservesExistingVerificationAndSessions(t *testing.T) {
	f := newGoogleLinkFixture(t)
	verifiedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := f.pool.Exec(context.Background(), "UPDATE users SET email_verified_at=$1,session_version=7 WHERE id=$2", verifiedAt, f.id); err != nil {
		t.Fatal(err)
	}
	if r := f.perform(f.email, "user", "google-subject"); r.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
	var actual time.Time
	var session int
	var password string
	if err := f.pool.QueryRow(context.Background(), "SELECT email_verified_at,session_version,password FROM users WHERE id=$1", f.id).Scan(&actual, &session, &password); err != nil {
		t.Fatal(err)
	}
	if !actual.Equal(verifiedAt) || session != 7 || password != "unused-test-hash" {
		t.Fatal("link altered existing verification, password or session version")
	}
}

func TestGoogleLinkRollsBackEveryWriteFailure(t *testing.T) {
	for _, stage := range []string{"verification", "audit", "commit"} {
		t.Run(stage, func(t *testing.T) {
			f := newGoogleLinkFixture(t)
			removeFailure := f.injectFailure(t, stage)
			response := f.perform(f.email, "user", "google-subject")
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			f.assertState(t, 0, 0, false)
			removeFailure()
			if retry := f.perform(f.email, "user", "google-subject"); retry.Code != http.StatusOK {
				t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
			}
			f.assertState(t, 1, 1, true)
		})
	}
}

func TestGoogleLinkSerializesConcurrentRetries(t *testing.T) {
	f := newGoogleLinkFixture(t)
	start, results := make(chan struct{}), make(chan int, 6)
	for i := 0; i < cap(results); i++ {
		go func() { <-start; results <- f.perform(f.email, "user", "same-subject").Code }()
	}
	close(start)
	for i := 0; i < cap(results); i++ {
		if status := <-results; status != http.StatusOK {
			t.Errorf("concurrent retry status=%d", status)
		}
	}
	f.assertState(t, 1, 1, true)
}

func TestGoogleLinkRejectsConcurrentDifferentSubjects(t *testing.T) {
	f := newGoogleLinkFixture(t)
	start, results := make(chan struct{}), make(chan int, 2)
	for _, subject := range []string{"subject-a", "subject-b"} {
		go func() { <-start; results <- f.perform(f.email, "user", subject).Code }()
	}
	close(start)
	counts := map[int]int{}
	for i := 0; i < 2; i++ {
		counts[<-results]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent results=%v", counts)
	}
	f.assertState(t, 1, 1, true)
}

func TestGoogleLinkCannotReuseAnotherCustomersIdentity(t *testing.T) {
	owner, other := newGoogleLinkFixture(t), newGoogleLinkFixture(t)
	if r := owner.perform(owner.email, "user", "shared-subject"); r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	if r := other.perform(other.email, "user", "shared-subject"); r.Code != http.StatusConflict {
		t.Fatalf("identity collision status=%d body=%s", r.Code, r.Body.String())
	}
	owner.assertState(t, 1, 1, true)
	other.assertState(t, 0, 0, false)
}

func TestGoogleLinkRejectsWrongRoleEmailAndMissingUser(t *testing.T) {
	f := newGoogleLinkFixture(t)
	for _, test := range []struct{ email, role string }{{f.email, "admin"}, {"different@selecto.test", "user"}} {
		if r := f.perform(test.email, test.role, "subject"); r.Code != http.StatusForbidden {
			t.Fatalf("guard status=%d body=%s", r.Code, r.Body.String())
		}
	}
	f.assertState(t, 0, 0, false)
	if _, err := f.pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", f.id); err != nil {
		t.Fatal(err)
	}
	if r := f.perform(f.email, "user", "subject"); r.Code != http.StatusUnauthorized {
		t.Fatalf("missing user status=%d body=%s", r.Code, r.Body.String())
	}
}

func TestGoogleLinkDatabaseFailureIsNotInvalidCredentials(t *testing.T) {
	f := newGoogleLinkFixture(t)
	// Clean up the fixture before deliberately closing the connection pool.
	if _, err := f.pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", f.id); err != nil {
		t.Fatal(err)
	}
	f.pool.Close()
	if r := f.perform(f.email, "user", "subject"); r.Code != http.StatusInternalServerError {
		t.Fatalf("database failure status=%d body=%s", r.Code, r.Body.String())
	}
}
