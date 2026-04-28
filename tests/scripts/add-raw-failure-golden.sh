#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

SAMPLE_ID="${1:-}"
EXPECT="${2:-}"
MANIFEST="tests/fixtures/raw_failure_golden.json"

if [[ -z "$SAMPLE_ID" || -z "$EXPECT" ]]; then
  echo "usage: $0 <sample-id> <tool_call|missing_tool|invalid_tool|auto>" >&2
  exit 1
fi

case "$EXPECT" in
  tool_call|missing_tool|invalid_tool|auto) ;;
  *)
    echo "invalid expectation: $EXPECT" >&2
    exit 1
    ;;
esac

if [[ ! -f "tests/raw_stream_samples/$SAMPLE_ID/meta.json" ]]; then
  echo "missing sample: tests/raw_stream_samples/$SAMPLE_ID/meta.json" >&2
  exit 1
fi

if [[ "$EXPECT" == "auto" ]]; then
  EXPECT="$(
    python3 - "$SAMPLE_ID" <<'PY'
import json
import pathlib
import re
import sys

sample_id = sys.argv[1]
root = pathlib.Path("tests/raw_stream_samples") / sample_id
meta = json.loads((root / "meta.json").read_text())
analysis = meta.get("analysis") or {}
category = str(analysis.get("category") or "")
source = str(meta.get("source") or "").lower()
raw = ""
stream_path = root / "upstream.stream.sse"
if stream_path.exists():
    raw = stream_path.read_text(errors="ignore")
raw_lower = raw.lower()

if "upstream_invalid_tool_call" in source:
    print("invalid_tool")
elif "upstream_missing_tool_call" in source:
    print("missing_tool")
elif category == "tool_syntax_candidate" or re.search(r"<\s*(tool_call|tool_calls|tool_use|invoke|function_call)\b", raw_lower):
    print("invalid_tool")
elif category in {"reasoning_without_visible_output", "empty_visible_output", "missing_finish", "empty_stream"}:
    print("missing_tool")
elif re.search(r'"(?:tool|tool_name|function)"\s*:', raw_lower) and re.search(r'"(?:arguments|input|params|parameters)"\s*:', raw_lower):
    print("invalid_tool")
else:
    raise SystemExit(f"cannot auto-classify {sample_id}: analysis.category={category!r}; pass an explicit expectation")
PY
  )"
fi

python3 - "$MANIFEST" "$SAMPLE_ID" "$EXPECT" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
sample_id = sys.argv[2]
expect = sys.argv[3]
rows = json.loads(path.read_text()) if path.exists() else []
rows = [row for row in rows if row.get("sample_id") != sample_id]
rows.append({"sample_id": sample_id, "expect": expect})
rows.sort(key=lambda row: row["sample_id"])
path.write_text(json.dumps(rows, ensure_ascii=False, indent=2) + "\n")
PY

go test ./internal/harness/claudecode -run "TestRawFailureSamplesReplayThroughClaudeCodeHarness/${SAMPLE_ID}$" -count=1
echo "[add-raw-failure-golden] added ${SAMPLE_ID} => ${EXPECT}"
