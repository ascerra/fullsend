package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidHarness(t *testing.T) {
	content := `
agent: agents/hello-world.md
image: registry.example.com/sandbox:v1
skills:
  - skills/hello-world-summary
validation_loop:
  script: scripts/validate-output.sh
  max_iterations: 1
runner_env:
  REPO_NAME: "${REPO_NAME}"
timeout_minutes: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "hello-world.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	h, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "agents/hello-world.md", h.Agent)
	assert.Equal(t, "registry.example.com/sandbox:v1", h.Image)
	assert.Equal(t, []string{"skills/hello-world-summary"}, h.Skills)
	require.NotNil(t, h.ValidationLoop)
	assert.Equal(t, "scripts/validate-output.sh", h.ValidationLoop.Script)
	assert.Equal(t, 1, h.ValidationLoop.MaxIterations)
	assert.Equal(t, `${REPO_NAME}`, h.RunnerEnv["REPO_NAME"])
	assert.Equal(t, 5, h.TimeoutMinutes)
}

func TestResolveRelativeTo_ImageUnchanged(t *testing.T) {
	h := &Harness{
		Agent: "agents/test.md",
		Image: "registry.example.com/sandbox:v1",
	}

	require.NoError(t, h.ResolveRelativeTo("/base/dir"))

	// Image is a registry reference, not a filesystem path — must not be resolved.
	assert.Equal(t, "registry.example.com/sandbox:v1", h.Image)
}

func TestLoad_MissingAgent(t *testing.T) {
	content := `
skills:
  - skills/hello-world-summary
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent field is required")
}

func TestLoad_ValidationLoopMissingScript(t *testing.T) {
	content := `
agent: agents/test.md
validation_loop:
  max_iterations: 3
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation_loop.script is required")
}

func TestLoad_HostFiles(t *testing.T) {
	content := `
agent: agents/test.md
host_files:
  - src: ${GOOGLE_APPLICATION_CREDENTIALS}
    dest: /tmp/workspace/.gcp-credentials.json
  - src: /etc/ssl/certs/ca-certificates.crt
    dest: /etc/ssl/certs/ca-certificates.crt
  - src: env/gcp-vertex.env
    dest: /tmp/workspace/.env.d/gcp-vertex.env
    expand: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	h, err := Load(path)
	require.NoError(t, err)

	require.Len(t, h.HostFiles, 3)
	assert.Equal(t, "${GOOGLE_APPLICATION_CREDENTIALS}", h.HostFiles[0].Src)
	assert.Equal(t, "/tmp/workspace/.gcp-credentials.json", h.HostFiles[0].Dest)
	assert.False(t, h.HostFiles[0].Expand)
	assert.Equal(t, "/etc/ssl/certs/ca-certificates.crt", h.HostFiles[1].Src)
	assert.Equal(t, "/etc/ssl/certs/ca-certificates.crt", h.HostFiles[1].Dest)
	assert.False(t, h.HostFiles[1].Expand)
	assert.Equal(t, "env/gcp-vertex.env", h.HostFiles[2].Src)
	assert.Equal(t, "/tmp/workspace/.env.d/gcp-vertex.env", h.HostFiles[2].Dest)
	assert.True(t, h.HostFiles[2].Expand)
}

func TestValidate_HostFileMissingSrc(t *testing.T) {
	content := `
agent: agents/test.md
host_files:
  - dest: /tmp/workspace/.gcp-credentials.json
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host_files[0]: src is required")
}

func TestValidate_HostFileMissingDest(t *testing.T) {
	content := `
agent: agents/test.md
host_files:
  - src: ${GOOGLE_APPLICATION_CREDENTIALS}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host_files[0]: dest is required")
}

func TestResolveRelativeTo(t *testing.T) {
	h := &Harness{
		Agent:      "agents/hello-world.md",
		Policy:     "policies/readonly.yaml",
		Skills:     []string{"skills/hello-world-summary"},
		PreScript:  "scripts/pre.sh",
		PostScript: "scripts/post.sh",
		AgentInput: "agent-input",
		ValidationLoop: &ValidationLoop{
			Script: "scripts/validate.sh",
		},
	}

	require.NoError(t, h.ResolveRelativeTo("/base/dir"))

	assert.Equal(t, "/base/dir/agents/hello-world.md", h.Agent)
	assert.Equal(t, "/base/dir/policies/readonly.yaml", h.Policy)
	assert.Equal(t, []string{"/base/dir/skills/hello-world-summary"}, h.Skills)
	assert.Equal(t, "/base/dir/scripts/pre.sh", h.PreScript)
	assert.Equal(t, "/base/dir/scripts/post.sh", h.PostScript)
	assert.Equal(t, "/base/dir/agent-input", h.AgentInput)
	assert.Equal(t, "/base/dir/scripts/validate.sh", h.ValidationLoop.Script)
}

func TestResolveRelativeTo_HostFiles(t *testing.T) {
	h := &Harness{
		Agent: "agents/test.md",
		HostFiles: []HostFile{
			{Src: "env/gcp-vertex.env", Dest: "/tmp/workspace/.env.d/gcp-vertex.env", Expand: true},
			{Src: "${GOOGLE_APPLICATION_CREDENTIALS}", Dest: "/tmp/workspace/.gcp-credentials.json"},
			{Src: "/absolute/path/file.txt", Dest: "/tmp/workspace/file.txt"},
		},
	}

	require.NoError(t, h.ResolveRelativeTo("/base/dir"))

	// Relative path without ${VAR} gets resolved.
	assert.Equal(t, "/base/dir/env/gcp-vertex.env", h.HostFiles[0].Src)
	// ${VAR} path is NOT resolved (expanded at bootstrap time).
	assert.Equal(t, "${GOOGLE_APPLICATION_CREDENTIALS}", h.HostFiles[1].Src)
	// Absolute path is unchanged.
	assert.Equal(t, "/absolute/path/file.txt", h.HostFiles[2].Src)
}

func TestResolveRelativeTo_AbsolutePathsUnchanged(t *testing.T) {
	h := &Harness{
		Agent: "/absolute/path/agent.md",
	}

	require.NoError(t, h.ResolveRelativeTo("/base/dir"))

	assert.Equal(t, "/absolute/path/agent.md", h.Agent)
}

func TestResolveRelativeTo_TraversalRejected(t *testing.T) {
	h := &Harness{Agent: "../../etc/shadow.md"}
	err := h.ResolveRelativeTo("/base/dir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolves outside fullsend directory")
}

func TestResolveRelativeTo_HostFileTraversalRejected(t *testing.T) {
	h := &Harness{
		Agent: "agents/test.md",
		HostFiles: []HostFile{
			{Src: "../../../etc/shadow", Dest: "/tmp/workspace/shadow"},
		},
	}
	err := h.ResolveRelativeTo("/base/dir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolves outside fullsend directory")
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading harness file")
}

func TestValidateRunnerEnv_UnsetVar(t *testing.T) {
	h := &Harness{
		Agent:     "test.md",
		RunnerEnv: map[string]string{"KEY": "${DEFINITELY_NOT_SET_VAR_XYZ}"},
	}
	err := h.ValidateRunnerEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEFINITELY_NOT_SET_VAR_XYZ")
}

func TestValidateRunnerEnv_LiteralValue(t *testing.T) {
	h := &Harness{
		Agent:     "test.md",
		RunnerEnv: map[string]string{"KEY": "literal_value"},
	}
	require.NoError(t, h.ValidateRunnerEnv())
}

func TestValidateRunnerEnv_HostFileSrcUnset(t *testing.T) {
	h := &Harness{
		Agent: "test.md",
		HostFiles: []HostFile{
			{Src: "${DEFINITELY_NOT_SET_VAR_XYZ}", Dest: "/tmp/dest"},
		},
	}
	err := h.ValidateRunnerEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEFINITELY_NOT_SET_VAR_XYZ")
}

func TestValidateRunnerEnv_PartialExpansion(t *testing.T) {
	h := &Harness{
		Agent:     "test.md",
		RunnerEnv: map[string]string{"ENDPOINT": "https://${DEFINITELY_NOT_SET_VAR_XYZ}/api"},
	}
	err := h.ValidateRunnerEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEFINITELY_NOT_SET_VAR_XYZ")
}

func TestValidate_AgentNameInvalid(t *testing.T) {
	h := &Harness{Agent: "agents/test';echo hack;echo '.md"}
	err := h.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestValidate_AgentNameValid(t *testing.T) {
	h := &Harness{Agent: "agents/hello-world_v2.md"}
	require.NoError(t, h.Validate())
}

func TestValidate_ModelInvalid(t *testing.T) {
	h := &Harness{Agent: "agents/test.md", Model: "sonnet'; echo hack"}
	err := h.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestValidate_ModelValid(t *testing.T) {
	for _, model := range []string{
		"claude-sonnet-4-6",
		"claude-sonnet-4-6@default",
		"claude-sonnet-4-6@20250514",
		"claude-opus-4-1@20250805",
	} {
		h := &Harness{Agent: "agents/test.md", Model: model}
		require.NoError(t, h.Validate(), "model %q should be valid", model)
	}
}

func TestValidate_NegativeTimeout(t *testing.T) {
	h := &Harness{Agent: "agents/test.md", TimeoutMinutes: -1}
	err := h.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_minutes must be non-negative")
}

func TestLoad_ModelField(t *testing.T) {
	content := `
agent: agents/test.md
model: sonnet
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	h, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "sonnet", h.Model)
}

func TestValidateFilesExist_MissingAgent(t *testing.T) {
	h := &Harness{Agent: "/nonexistent/agent.md"}
	err := h.ValidateFilesExist()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent")
}

func TestValidateFilesExist_MissingSkill(t *testing.T) {
	dir := t.TempDir()
	agentFile := filepath.Join(dir, "agent.md")
	require.NoError(t, os.WriteFile(agentFile, []byte("agent"), 0o644))

	h := &Harness{
		Agent:  agentFile,
		Skills: []string{"/nonexistent/skill"},
	}
	err := h.ValidateFilesExist()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills[0]")
}

func TestValidateFilesExist_SkipsVarPaths(t *testing.T) {
	dir := t.TempDir()
	agentFile := filepath.Join(dir, "agent.md")
	require.NoError(t, os.WriteFile(agentFile, []byte("agent"), 0o644))

	h := &Harness{
		Agent: agentFile,
		HostFiles: []HostFile{
			{Src: "${SOME_VAR}", Dest: "/tmp/dest"},
		},
	}
	// Should not error — ${VAR} paths are expanded at bootstrap time.
	require.NoError(t, h.ValidateFilesExist())
}
