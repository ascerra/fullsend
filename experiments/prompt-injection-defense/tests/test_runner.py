# tests/test_runner.py
from unittest.mock import patch, MagicMock
from pathlib import Path

import pytest
from defenses.interface import DefenseResult, Attack
from runner import run_matrix, format_results_table, majority_outcome


def test_format_results_table():
    results = {
        ("benign", "no_defense"): [
            DefenseResult(detected=False, explanation="clean"),
            DefenseResult(detected=False, explanation="clean"),
            DefenseResult(detected=False, explanation="clean"),
        ],
        ("benign", "spotlighting"): [
            DefenseResult(detected=False, explanation="clean"),
            DefenseResult(detected=False, explanation="clean"),
            DefenseResult(detected=False, explanation="clean"),
        ],
    }
    table = format_results_table(results)
    lines = table.strip().split("\n")
    # Header + separator + 1 data row
    assert len(lines) == 3
    # Header has all defense columns
    assert "no_defense" in lines[0]
    assert "spotlighting" in lines[0]
    # Data row has attack name and results
    assert "benign" in lines[2]
    assert "clean" in lines[2]


def test_majority_vote_logic():
    results_clean = [
        DefenseResult(detected=False, explanation=""),
        DefenseResult(detected=False, explanation=""),
        DefenseResult(detected=True, explanation=""),
    ]
    assert majority_outcome(results_clean) == "clean"

    results_detected = [
        DefenseResult(detected=True, explanation=""),
        DefenseResult(detected=True, explanation=""),
        DefenseResult(detected=False, explanation=""),
    ]
    assert majority_outcome(results_detected) == "detected"
