package main

import (
	"os"
	"testing"
)

func withInstance(t *testing.T, name string) {
	t.Helper()
	previousFlag := *instanceName
	previousDefault := *instanceIsDefault
	previousEnv, hadEnv := os.LookupEnv("WUZAPI_INSTANCE")
	previousDefaultEnv, hadDefaultEnv := os.LookupEnv("WUZAPI_INSTANCE_DEFAULT")
	*instanceName = name
	*instanceIsDefault = false
	os.Unsetenv("WUZAPI_INSTANCE_DEFAULT")
	t.Cleanup(func() {
		*instanceName = previousFlag
		*instanceIsDefault = previousDefault
		if hadEnv {
			os.Setenv("WUZAPI_INSTANCE", previousEnv)
		} else {
			os.Unsetenv("WUZAPI_INSTANCE")
		}
		if hadDefaultEnv {
			os.Setenv("WUZAPI_INSTANCE_DEFAULT", previousDefaultEnv)
			return
		}
		os.Unsetenv("WUZAPI_INSTANCE_DEFAULT")
	})
}

func TestOwnsInstance(t *testing.T) {
	t.Run("given sharding is off then every user is owned", func(t *testing.T) {
		withInstance(t, "")
		os.Unsetenv("WUZAPI_INSTANCE")
		for _, owner := range []string{"", "listener", "sender"} {
			if !ownsInstance(owner) {
				t.Fatalf("expected instance %q to be owned when sharding is off", owner)
			}
		}
	})

	t.Run("given a named instance then only its own users are owned", func(t *testing.T) {
		withInstance(t, "listener")
		if !ownsInstance("listener") {
			t.Fatal("expected the instance to own its own users")
		}
		if ownsInstance("sender") {
			t.Fatal("expected a sibling instance's users to be refused")
		}
	})

	t.Run("given the instance is not the default then unassigned users are refused", func(t *testing.T) {
		withInstance(t, "sender")
		if ownsInstance("") {
			t.Fatal("expected unassigned users to belong to the default instance only")
		}
	})

	t.Run("given the default instance then unassigned users are owned", func(t *testing.T) {
		withInstance(t, "listener")
		*instanceIsDefault = true
		if !ownsInstance("") {
			t.Fatal("expected the default instance to own users created before the split")
		}
	})

	t.Run("given the default is set by environment then it counts", func(t *testing.T) {
		withInstance(t, "listener")
		os.Setenv("WUZAPI_INSTANCE_DEFAULT", "true")
		if !isDefaultInstance() {
			t.Fatal("expected WUZAPI_INSTANCE_DEFAULT to enable default ownership")
		}
	})

	t.Run("must ignore surrounding blanks on both sides", func(t *testing.T) {
		withInstance(t, "  sender  ")
		if !ownsInstance(" sender ") {
			t.Fatal("expected padded names to compare equal")
		}
	})
}

func TestCurrentInstance(t *testing.T) {
	t.Run("given no flag then the environment variable is used", func(t *testing.T) {
		withInstance(t, "")
		os.Setenv("WUZAPI_INSTANCE", "sender")
		if got := currentInstance(); got != "sender" {
			t.Fatalf("expected sender, got %q", got)
		}
		if !shardingEnabled() {
			t.Fatal("expected sharding to be enabled")
		}
	})

	t.Run("given both then the flag wins", func(t *testing.T) {
		withInstance(t, "listener")
		os.Setenv("WUZAPI_INSTANCE", "sender")
		if got := currentInstance(); got != "listener" {
			t.Fatalf("expected listener, got %q", got)
		}
	})

	t.Run("given neither then sharding is off", func(t *testing.T) {
		withInstance(t, "")
		os.Unsetenv("WUZAPI_INSTANCE")
		if currentInstance() != "" {
			t.Fatal("expected an empty instance name")
		}
		if shardingEnabled() {
			t.Fatal("expected sharding to be disabled")
		}
	})
}

func TestForgetInstanceOwnership(t *testing.T) {
	t.Run("must drop the cached assignment", func(t *testing.T) {
		instanceOwnership.Set("token-a", "listener", 0)
		forgetInstanceOwnership("token-a")
		if _, found := instanceOwnership.Get("token-a"); found {
			t.Fatal("expected the cached ownership to be gone")
		}
	})
}
