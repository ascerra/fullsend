#!/usr/bin/env python3
"""Unit tests for unicode_posttool.py hook."""

import json
import subprocess
import sys
import unittest
from pathlib import Path

HOOK = str(Path(__file__).parent / "unicode_posttool.py")


def run_hook(tool_result: str | None = None, stdin_raw: str | None = None) -> tuple[int, str, str]:
    """Run the hook script and return (exit_code, stdout, stderr)."""
    if stdin_raw is None:
        if tool_result is None:
            stdin_raw = ""
        else:
            stdin_raw = json.dumps({"tool_name": "Read", "tool_result": tool_result})
    proc = subprocess.run(
        [sys.executable, HOOK],
        input=stdin_raw,
        capture_output=True,
        text=True,
        timeout=10,
    )
    return proc.returncode, proc.stdout, proc.stderr


class TestCleanInput(unittest.TestCase):
    def test_clean_text_passes_through(self):
        rc, stdout, _ = run_hook("Hello, world!")
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")

    def test_empty_stdin(self):
        rc, stdout, _ = run_hook(stdin_raw="")
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")

    def test_non_json_stdin(self):
        rc, stdout, _ = run_hook(stdin_raw="not json")
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")

    def test_non_dict_json(self):
        rc, stdout, _ = run_hook(stdin_raw=json.dumps([1, 2, 3]))
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")

    def test_non_string_tool_result(self):
        rc, stdout, _ = run_hook(stdin_raw=json.dumps({"tool_result": 42}))
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")

    def test_empty_tool_result(self):
        rc, stdout, _ = run_hook(tool_result="")
        self.assertEqual(rc, 0)
        self.assertEqual(stdout, "")


class TestZeroWidth(unittest.TestCase):
    def test_zero_width_space_stripped(self):
        rc, stdout, _ = run_hook("hello\u200bworld")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "helloworld")
        self.assertIn("zero_width", out["metadata"]["categories"])

    def test_soft_hyphen_stripped(self):
        rc, stdout, _ = run_hook("pass\u00adword")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "password")

    def test_word_joiner_stripped(self):
        rc, stdout, _ = run_hook("test\u2060data")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "testdata")
        self.assertIn("zero_width", out["metadata"]["categories"])


class TestBidiOverride(unittest.TestCase):
    def test_bidi_override_stripped(self):
        rc, stdout, _ = run_hook("abc\u202edef")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "abcdef")
        self.assertIn("bidi_override", out["metadata"]["categories"])

    def test_bidi_isolate_stripped(self):
        rc, stdout, _ = run_hook("abc\u2066def\u2069")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "abcdef")
        self.assertIn("bidi_override", out["metadata"]["categories"])


class TestTagCharacters(unittest.TestCase):
    def test_tag_chars_stripped(self):
        # Tag-encode "HI" (U+E0048 U+E0049)
        payload = "clean\U000e0048\U000e0049text"
        rc, stdout, _ = run_hook(payload)
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "cleantext")
        self.assertIn("tag_char", out["metadata"]["categories"])

    def test_tag_chars_always_exit_zero(self):
        payload = "\U000e0048\U000e0049"
        rc, _, _ = run_hook(payload)
        self.assertEqual(rc, 0)


class TestNullBytes(unittest.TestCase):
    def test_null_bytes_stripped(self):
        rc, stdout, _ = run_hook("hello\x00world")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "helloworld")
        self.assertIn("null_byte", out["metadata"]["categories"])


class TestAnsiEscape(unittest.TestCase):
    def test_ansi_color_stripped(self):
        rc, stdout, _ = run_hook("hello\x1b[31mred\x1b[0m")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "hellored")
        self.assertIn("ansi_escape", out["metadata"]["categories"])


class TestNFKC(unittest.TestCase):
    def test_fullwidth_normalized(self):
        # Fullwidth A = U+FF21
        rc, stdout, _ = run_hook("\uff21\uff22\uff23")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "ABC")
        self.assertIn("fullwidth", out["metadata"]["categories"])


class TestVariationSelector(unittest.TestCase):
    def test_variation_selector_stripped(self):
        rc, stdout, _ = run_hook("test\ufe0fdata")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "testdata")
        self.assertIn("variation_selector", out["metadata"]["categories"])


class TestProtocol(unittest.TestCase):
    def test_no_decoded_text_in_stdout(self):
        """C1: Decoded tag char text must NOT appear in stdout (injection vector)."""
        # Tag-encode "INJECT" → U+E0049 U+E004E U+E004A U+E0045 U+E0043 U+E0054
        payload = "\U000e0049\U000e004e\U000e004a\U000e0045\U000e0043\U000e0054"
        rc, stdout, _ = run_hook(f"clean{payload}text")
        self.assertEqual(rc, 0)
        self.assertNotIn("INJECT", stdout)
        out = json.loads(stdout)
        self.assertEqual(out["tool_result"], "cleantext")

    def test_always_returns_tool_result(self):
        rc, stdout, _ = run_hook("has\u200bzero\u200bwidth")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertIn("tool_result", out)

    def test_metadata_present(self):
        rc, stdout, _ = run_hook("has\u200bzero")
        self.assertEqual(rc, 0)
        out = json.loads(stdout)
        self.assertIn("metadata", out)
        self.assertIn("unicode_findings", out["metadata"])
        self.assertIn("categories", out["metadata"])


if __name__ == "__main__":
    unittest.main()
