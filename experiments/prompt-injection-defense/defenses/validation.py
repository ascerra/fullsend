# defenses/validation.py
import json

from defenses.interface import DefenseResult


def validate_response(response_text: str, injection_goal: str) -> DefenseResult:
    """Two-layer validation: structural then semantic."""
    # Layer 1: Structural
    try:
        data = json.loads(response_text)
    except (json.JSONDecodeError, TypeError):
        return DefenseResult(
            detected=True,
            explanation=f"Response is not valid JSON: {response_text[:200]}",
            raw_output=response_text,
        )

    if "assessment" not in data:
        return DefenseResult(
            detected=True,
            explanation="Response JSON missing 'assessment' field",
            raw_output=response_text,
        )

    if data["assessment"] not in ("clean", "suspicious"):
        return DefenseResult(
            detected=True,
            explanation=f"Response 'assessment' has invalid value: {data['assessment']}",
            raw_output=response_text,
        )

    # Layer 2: Semantic — check if the injection goal appears in the output
    if injection_goal and injection_goal != "none":
        goal_lower = injection_goal.lower()
        response_lower = response_text.lower()

        goal_terms = [
            term.strip()
            for term in goal_lower.replace('"', " ").replace("'", " ").split()
            if len(term.strip()) > 2  # skip very short words
        ]
        for term in goal_terms:
            if term in (
                "output", "instead", "following", "should", "would",
                "nothing", "the", "word", "json", "format", "return",
                "text", "that", "this", "with", "from", "your", "not",
            ):
                continue  # skip generic instruction words
            if term in response_lower and term not in (
                "clean", "suspicious", "commit", "message", "assessment", "reasoning",
            ):
                return DefenseResult(
                    detected=True,
                    explanation=f"Semantic check: injection_goal term '{term}' found in response",
                    raw_output=response_text,
                )

    return DefenseResult(
        detected=False,
        explanation="Response passed structural and semantic validation",
        raw_output=response_text,
    )
