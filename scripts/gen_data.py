#!/usr/bin/env python3

import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DATA_DIR = ROOT / "data"


OVERRIDES = {
    "v1": {
        23: ["Michael Bennet", "John Hickenlooper"],
        29: ["Lauren Boebert"],
        30: ["Mike Johnson"],
        38: ["Donald J. Trump", "Donald Trump", "President Trump"],
        39: ["JD Vance", "J.D. Vance", "James David Vance"],
        57: ["John G. Roberts, Jr.", "John Roberts", "Chief Justice John Roberts"],
        61: ["Jared Polis", "Governor Jared Polis"],
        62: ["Denver"],
    },
    "v2": {
        20: ["Michael Bennet", "John Hickenlooper"],
        23: ["Lauren Boebert"],
        28: ["Donald J. Trump", "Donald Trump", "President Trump"],
        29: ["JD Vance", "J.D. Vance", "James David Vance"],
        39: ["9", "nine"],
        40: ["John G. Roberts, Jr.", "John Roberts", "Chief Justice John Roberts"],
        43: ["Jared Polis", "Governor Jared Polis"],
        44: ["Denver"],
        46: ["Republican", "Republican Party"],
        47: ["Mike Johnson"],
    },
}


QUESTION_RE = re.compile(r"^(\d+)\.\s+(.*)$")
PAGE_RE = re.compile(r"^\d+\s+of\s+\d+$|^-\d+-$")


def normalize_line(line: str) -> str:
    line = (
        line.replace("’", "'")
        .replace("“", '"')
        .replace("”", '"')
        .replace("–", "-")
    )
    line = line.replace("\t", " ")
    line = re.sub(r"\s+", " ", line).strip()
    line = re.sub(r"(\d+)\s+(st|nd|rd|th)\b", r"\1\2", line)
    line = re.sub(r"'\s+s\b", "'s", line)
    line = line.replace("may study just the questions that have been marked with an asterisk.", "").strip()
    return line


def tidy_text(text: str) -> str:
    text = re.sub(r"(\d+)\s+(st|nd|rd|th)\b", r"\1\2", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def clean_lines(raw: str) -> list[str]:
    lines = []
    for original in raw.splitlines():
        line = normalize_line(original)
        if not line:
            continue
        if PAGE_RE.match(line):
            continue
        if line in {
            "uscis.gov/citizenship",
            "www.uscis.gov",
            "(rev. 01/19)",
        }:
            continue
        if line.startswith("* If you are 65 years old"):
            continue
        if line.startswith("Listed below are the 128 civics questions"):
            continue
        if line.startswith("These questions cover important topics"):
            continue
        if line.startswith("test questions. You must answer"):
            continue
        if line.startswith("On the civics test, some answers may change"):
            continue
        if line.startswith("Although USCIS is aware"):
            continue
        if line.startswith("65/20 Special Consideration"):
            continue
        if line.startswith("If you are 65 years old or older"):
            continue
        if line.startswith("The 100 civics (history and government)"):
            continue
        if line.startswith("is an oral test and the USCIS Officer"):
            continue
        if line.startswith("On the naturalization test, some answers"):
            continue
        if line.startswith("make sure that you know the most current"):
            continue
        if line.startswith("answer.") or line.startswith("who is serving"):
            continue
        if line.startswith("applicants are encouraged"):
            continue
        if line.startswith("For a complete list of tribes"):
            continue
        lines.append(line)
    return lines


def required_answer_count(question: str) -> int:
    lower = question.lower()
    if "describe one of them" in lower:
        return 1
    for count, token in [(5, "name five"), (4, "name four"), (3, "name three"), (2, "name two"), (2, "what are two"), (3, "what are three"), (2, "give two"), (3, "give three"), (2, "examples of")]:
        if token in lower:
            return count
    return 1


def parse_questions(version: str, raw: str) -> list[dict]:
    lines = clean_lines(raw)
    questions = []
    current = None
    collecting_answers = False

    def flush():
        nonlocal current
        if current:
            current["question"] = tidy_text(re.sub(r"\s*\*\s*$", "", current["question"]).strip())
            current["answers"] = [tidy_text(re.sub(r"\s*\*\s*$", "", a).strip()) for a in current["answers"] if a.strip()]
            current["requiredAnswers"] = required_answer_count(current["question"])
            questions.append(current)
            current = None

    for line in lines:
        match = QUESTION_RE.match(line)
        if match:
            flush()
            current = {
                "id": int(match.group(1)),
                "question": match.group(2),
                "answers": [],
            }
            collecting_answers = False
            continue
        if current is None:
            continue
        if line.startswith("•") or line.startswith("▪"):
            current["answers"].append(line[1:].strip())
            collecting_answers = True
            continue
        if collecting_answers and current["answers"]:
            current["answers"][-1] = f'{current["answers"][-1]} {line}'.strip()
        else:
            current["question"] = f'{current["question"]} {line}'.strip()

    flush()

    overrides = OVERRIDES[version]
    for question in questions:
        if question["id"] in overrides:
            question["answers"] = overrides[question["id"]]

    return questions


def main() -> None:
    DATA_DIR.mkdir(exist_ok=True)
    sources = {
        "v1": Path("/tmp/uscis-128.txt"),
        "v2": Path("/tmp/uscis-100.txt"),
    }
    for version, source in sources.items():
        questions = parse_questions(version, source.read_text())
        payload = {
            "version": version,
            "locality": "Parker, Colorado 80134",
            "asOf": "2026-03-16",
            "questions": questions,
        }
        (DATA_DIR / f"{version}.json").write_text(json.dumps(payload, indent=2))
        print(version, len(questions))


if __name__ == "__main__":
    main()
