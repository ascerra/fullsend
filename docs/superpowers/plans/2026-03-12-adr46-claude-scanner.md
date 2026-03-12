# ADR-0046 Claude Scanner Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a shell script that uses `claude` CLI to analyze Tekton tasks for ADR compliance, and run it against the modelcar-oci-ta task to validate the approach.

**Architecture:** A thin shell script passes ADR text + task YAML to `claude -p`, which produces a prose analysis of violations and fixes. Validated by comparing output against a hand-written expected analysis.

**Tech Stack:** Bash, `claude` CLI, `gh` CLI (for fetching ADR content)

---

### Task 1: Write the expected analysis

Before building anything, write our human judgment of what violations exist. This is the benchmark.

**Files:**
- Create: `experiments/adr46-claude-scanner/expected/modelcar-oci-ta.md`

- [ ] **Step 1: Create directory and write the expected analysis**

```bash
mkdir -p experiments/adr46-claude-scanner/expected
```

Write the hand-crafted expected output based on reading ADR-0046 and the modelcar-oci-ta task. Cover every step: whether it complies, is exempt, or violates, and what to do about each violation.

```markdown
# Expected Analysis: modelcar-oci-ta vs ADR-0046

## ADR-0046 requirement

All Tekton task steps should use the common task runner image
(`quay.io/konflux-ci/task-runner`) unless they have a legitimate exemption.
The ADR explicitly carves out an exception for use-case-oriented images
like build-trusted-artifacts.

## Step-by-step analysis

### use-trusted-artifact — EXEMPT

Image: `quay.io/konflux-ci/build-trusted-artifacts`

ADR-0046 states: "The Task Runner image does not replace the more specialized
use-case-oriented images." The build-trusted-artifacts image is a use-case-oriented
image (Trusted Artifacts), not a tool-oriented one. Additionally, the ADR notes
that "Trusted Artifacts variants of Tasks would still use separate steps to
create/use the artifacts." This step is legitimately exempt.

### download-model-files — VIOLATION

Image: `quay.io/konflux-ci/oras`

This step uses the oras tool-oriented container image. ADR-0046 calls for
gradually deprecating "all the current tool-oriented container images" in
favor of the task runner. The step uses `oras` and `retry`, both of which
are available in the task runner image.

**Fix:** Replace the image reference with the task runner image. No other
changes needed — `oras` and `retry` are already installed.

### create-modelcar-base-image — VIOLATION

Image: `quay.io/konflux-ci/release-service-utils`

This step uses `get-image-architectures`, `oras`, and `jq`. While `oras`
and `jq` are in the task runner, `get-image-architectures` is not.

**Fix:** The `get-image-architectures` tool needs to be added to the task
runner image first. Once added, replace the image reference with the task
runner image.

### copy-model-files — VIOLATION

Image: `registry.access.redhat.com/ubi9/python-311`

This step uses `python3` and `pip install olot`. The task runner has `python3`
but `olot` is a Python package installed at runtime via pip, not a standalone
tool baked into any image.

**Fix:** This is a gray area. `olot` is installed via `pip` at runtime, so
swapping to the task runner image would work if `pip` is available. However,
`olot` may not meet the ADR's inclusion criteria for the task runner ("be an
actual standalone tool", "follow a versioning scheme") since it's a Python
library. The image swap may be possible without adding `olot` to the task
runner — just use the task runner's `python3` and keep the `pip install`
in the script. Alternatively, if `olot` qualifies as a tool, propose adding
it to the task runner.

### push-image — VIOLATION

Image: `quay.io/konflux-ci/oras`

Same as download-model-files. Uses `oras`, `select-oci-auth`, and `retry`,
all available in the task runner.

**Fix:** Replace the image reference with the task runner image. No other
changes needed.

### sbom-generate — VIOLATION

Image: `quay.io/konflux-ci/mobster`

This step uses `mobster`, which is not in the task runner image.

**Fix:** `mobster` needs to be added to the task runner image first. It
appears to meet the ADR's inclusion criteria (standalone tool with versioned
releases). Once added, replace the image reference.

### upload-sbom — VIOLATION

Image: `quay.io/konflux-ci/appstudio-utils`

This step uses `cosign`, `select-oci-auth`, and `retry`, all available in
the task runner. ADR-0046 specifically calls out `appstudio-utils` as
problematic ("breaks good secure supply chain practices").

**Fix:** Replace the image reference with the task runner image. No other
changes needed.

### report-sbom-url — VIOLATION

Image: `quay.io/konflux-ci/yq`

This step uses the yq tool-oriented container image, but the script itself
only uses basic shell utilities (`sha256sum`, `awk`, bash string manipulation)
— it does not actually invoke the `yq` binary. All tools used are available
in the task runner.

**Fix:** Replace the image reference with the task runner image. No other
changes needed — the step doesn't even use `yq`.

## Summary

- 1 step exempt (use-trusted-artifact)
- 7 steps in violation
- 4 violations fixable today by swapping the image (download-model-files, push-image, upload-sbom, report-sbom-url)
- 2 violations require adding tools to the task runner first (create-modelcar-base-image needs `get-image-architectures`, sbom-generate needs `mobster`)
- 1 violation is a gray area (copy-model-files — may work with a simple image swap if pip is available, or may need `olot` added)
```

- [ ] **Step 2: Commit**

```bash
git add experiments/adr46-claude-scanner/expected/modelcar-oci-ta.md
git commit -m "docs: add hand-written expected analysis for modelcar-oci-ta"
```

---

### Task 2: Fetch ADR and task YAML as local files

The scan script needs local copies. Fetch them and commit as test fixtures.

**Files:**
- Create: `experiments/adr46-claude-scanner/fixtures/adr-0046.md`
- Create: `experiments/adr46-claude-scanner/fixtures/modelcar-oci-ta.yaml`

- [ ] **Step 1: Create directories and fetch ADR-0046**

```bash
mkdir -p experiments/adr46-claude-scanner/fixtures
gh api repos/konflux-ci/architecture/contents/ADR/0046-common-task-runner-image.md \
  -q '.content' | base64 -d \
  > experiments/adr46-claude-scanner/fixtures/adr-0046.md
```

- [ ] **Step 2: Copy the task fixture from experiment 001**

```bash
cp experiments/adr46-scanner/tests/fixtures/modelcar-oci-ta.yaml \
   experiments/adr46-claude-scanner/fixtures/modelcar-oci-ta.yaml
```

- [ ] **Step 3: Commit**

```bash
git add experiments/adr46-claude-scanner/fixtures/
git commit -m "feat: add ADR-0046 and modelcar-oci-ta as local fixtures"
```

---

### Task 3: Write the scan script

**Files:**
- Create: `experiments/adr46-claude-scanner/scan.sh`

- [ ] **Step 1: Write scan.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 <adr-file> <task-yaml-file>" >&2
    exit 1
}

[[ $# -eq 2 ]] || usage

adr_file="$1"
task_file="$2"

[[ -f "$adr_file" ]] || { echo "Error: ADR file not found: $adr_file" >&2; exit 1; }
[[ -f "$task_file" ]] || { echo "Error: Task file not found: $task_file" >&2; exit 1; }

adr_content=$(<"$adr_file")
task_content=$(<"$task_file")

prompt="The following is an Architectural Decision Record (ADR) from the konflux-ci project:

---
${adr_content}
---

The following is a Tekton task definition from the same project:

---
${task_content}
---

Analyze this task for compliance with the ADR. For each violation you find, describe:
- What: which step violates the ADR and what it currently does
- Why: what the ADR requires and why this step doesn't comply
- Fix: what should be done to bring the step into compliance

If a step is legitimately exempt per the ADR, note that and explain why."

claude -p --model claude-sonnet-4-20250514 "$prompt"
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x experiments/adr46-claude-scanner/scan.sh
```

- [ ] **Step 3: Commit**

```bash
git add experiments/adr46-claude-scanner/scan.sh
git commit -m "feat: add claude-based ADR drift scan script"
```

---

### Task 4: Run the scan and capture results

- [ ] **Step 1: Run the scanner**

```bash
cd experiments/adr46-claude-scanner
mkdir -p results
./scan.sh fixtures/adr-0046.md fixtures/modelcar-oci-ta.yaml \
  | tee results/modelcar-oci-ta.md
```

- [ ] **Step 2: Review the output**

Read `results/modelcar-oci-ta.md` and compare against `expected/modelcar-oci-ta.md` using the rubric from the spec:

1. **Violation detection** — Did it find all 7 violations?
2. **Exemption recognition** — Did it correctly exempt use-trusted-artifact?
3. **Fix quality** — Did it distinguish "swap today" from "add tooling first"?
4. **Unexpected insights** — Anything we didn't anticipate?

- [ ] **Step 3: Commit results**

```bash
git add experiments/adr46-claude-scanner/results/modelcar-oci-ta.md
git commit -m "docs: add claude scan results for modelcar-oci-ta"
```

---

### Task 5: Write the experiment log

**Files:**
- Create: `docs/experiments/002-adr46-claude-scanner.md`

- [ ] **Step 1: Write the experiment narrative**

Document: hypothesis, setup, method, results (referencing the artifacts), analysis against the rubric, comparison with expected output, and conclusions about whether the approach generalizes.

The structure should mirror experiment 001's format at `docs/experiments/001-adr46-drift-scanner.md`.

- [ ] **Step 2: Commit**

```bash
git add docs/experiments/002-adr46-claude-scanner.md
git commit -m "docs: add experiment log for claude-based ADR drift scanner"
```

---

## Execution notes

- Tasks 1-3 are independent and can be parallelized.
- Task 4 depends on tasks 1, 2, and 3 (needs the expected analysis to compare against, the fixtures, and the script).
- Task 5 depends on task 4 (needs the results to write about).
- All work happens in the worktree at `.worktrees/experiment-002-adr46-claude` on branch `experiment/002-adr46-claude-scanner`.
