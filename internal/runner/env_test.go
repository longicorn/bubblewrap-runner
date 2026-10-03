package runner

import (
	"strings"
	"testing"
)

func TestBuildEnvironmentPrecedenceAndManagedValues(t *testing.T) {
	t.Setenv("BWRUN_ENV_KEEP", "host")
	t.Setenv("BWRUN_ENV_DROP", "secret")
	t.Setenv("BWRUN_ENV_OVERRIDE", "host")
	for _, test := range []struct {
		name, want string
		present    bool
	}{
		{"BWRUN_ENV_KEEP", "host", true},
		{"BWRUN_ENV_DROP", "", false},
		{"BWRUN_ENV_OVERRIDE", "project", true},
		{"HOME", "/sandbox/home", true},
		{"PWD", "/work/project", true},
		{sandboxMarkerEnv, "1", true},
	} {
		layers := []configLayer{{rank: 2, config: Config{Env: EnvConfig{
			Deny: []string{"BWRUN_ENV_DROP"},
			Set:  map[string]string{"BWRUN_ENV_OVERRIDE": "project"},
		}}}}
		env, err := buildEnvironment(layers, "/sandbox/home", "/work/project", "/usr/bin")
		if err != nil {
			t.Fatal(err)
		}
		got, ok := lookupEnv(env, test.name)
		if ok != test.present || got != test.want {
			t.Errorf("%s = %q, %v; want %q, %v", test.name, got, ok, test.want, test.present)
		}
	}
}

func TestBuildEnvironmentRejectsInvalidAndManagedOverrides(t *testing.T) {
	for _, envConfig := range []EnvConfig{
		{Pass: []string{"BAD=NAME"}},
		{Deny: []string{""}},
		{Set: map[string]string{"HOME": "/tmp"}},
		{Set: map[string]string{sandboxMarkerEnv: "0"}},
	} {
		if _, err := buildEnvironment([]configLayer{{config: Config{Env: envConfig}}}, "/home/u", "/work", ""); err == nil {
			t.Errorf("buildEnvironment accepted %#v", envConfig)
		}
	}
}

func TestBuildEnvironmentExplicitPassAndPath(t *testing.T) {
	t.Setenv("BWRUN_ENV_EXPLICIT", "available")
	env, err := buildEnvironment([]configLayer{{config: Config{Env: EnvConfig{Pass: []string{"BWRUN_ENV_EXPLICIT"}}}}}, "/home/u", "/work", "/custom/bin")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := lookupEnv(env, "BWRUN_ENV_EXPLICIT"); !ok || got != "available" {
		t.Fatalf("explicit pass = %q, %v", got, ok)
	}
	if got, ok := lookupEnv(env, "PATH"); !ok || !strings.Contains(got, "/custom/bin") {
		t.Fatalf("PATH = %q, %v", got, ok)
	}
}

func lookupEnv(env []string, name string) (string, bool) {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == name {
			return value, true
		}
	}
	return "", false
}

func TestBuildEnvironmentIsSorted(t *testing.T) {
	env, err := buildEnvironment(nil, "/home/u", "/work", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(env); i++ {
		if strings.SplitN(env[i-1], "=", 2)[0] > strings.SplitN(env[i], "=", 2)[0] {
			t.Fatalf("environment is not sorted: %v", env)
		}
	}
}
