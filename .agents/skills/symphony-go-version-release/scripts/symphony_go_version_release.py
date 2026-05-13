#!/usr/bin/env python3
"""symphony-go version/release helper.

Default mode is dry-run. File writes require --write.
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any


CHANGELOG_CANDIDATES = ("ChangeLog.md", "changeLog.md")
DEFAULT_CHANGELOG = "ChangeLog.md"
SEMVER_RE = re.compile(
    r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)"
    r"(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?"
    r"(?:\+[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?$"
)
CATEGORIES = ["feature", "optimization", "bugFix", "note", "script"]


@dataclass
class Finding:
    severity: str
    path: str
    message: str
    detail: str = ""

    def as_dict(self) -> dict[str, str]:
        return {
            "severity": self.severity,
            "path": self.path,
            "message": self.message,
            "detail": self.detail,
        }


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_text(path: Path, text: str) -> None:
    path.write_text(text, encoding="utf-8")


def emit(data: dict[str, Any], as_json: bool) -> None:
    if as_json:
        print(json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True))
        return
    for key, value in data.items():
        if isinstance(value, (dict, list)):
            print(f"{key}: {json.dumps(value, ensure_ascii=False, indent=2)}")
        else:
            print(f"{key}: {value}")


def ensure_repo(repo: str) -> Path:
    path = Path(repo).expanduser().resolve()
    if not path.exists():
        raise SystemExit(f"repo does not exist: {path}")
    return path


def find_changelog(repo: Path) -> Path:
    for name in CHANGELOG_CANDIDATES:
        path = repo / name
        if path.exists():
            return path
    return repo / DEFAULT_CHANGELOG


def rel(repo: Path, path: Path) -> str:
    try:
        return path.relative_to(repo).as_posix()
    except ValueError:
        return path.as_posix()


def validate_semver(value: str) -> None:
    if not SEMVER_RE.fullmatch(value.strip()):
        raise SystemExit(f"invalid SemVer: {value!r}; expected vX.Y.Z")


def find_unreleased_bounds(text: str) -> tuple[int, int, int] | None:
    match = re.search(r"(?m)^##\s+Unreleased\s*$", text)
    if not match:
        return None
    header_start = match.start()
    content_start = match.end()
    next_match = re.search(r"(?m)^#{2,3}\s+v\d+\.\d+\.\d+", text[content_start:])
    content_end = len(text) if not next_match else content_start + next_match.start()
    return header_start, content_start, content_end


def ensure_unreleased(text: str) -> str:
    if re.search(r"(?m)^##\s+Unreleased\s*$", text):
        return text
    return "## Unreleased\n\n" + text.lstrip()


def next_item_number(section: str) -> int:
    numbers = [int(n) for n in re.findall(r"(?m)^(\d+)\.\s+", section)]
    return max(numbers, default=0) + 1


def add_entry_to_block(block: str, category: str, entry: str) -> str:
    heading = f"#### {category}:"
    heading_match = re.search(rf"(?m)^{re.escape(heading)}\s*$", block)
    if heading_match:
        section_start = heading_match.end()
        next_heading = re.search(r"(?m)^####\s+\w+:", block[section_start:])
        section_end = len(block) if not next_heading else section_start + next_heading.start()
        section = block[section_start:section_end]
        item = f"{next_item_number(section)}. {entry}\n"
        prefix = block[:section_end].rstrip() + "\n"
        suffix = block[section_end:].lstrip("\n")
        return prefix + item + ("\n" + suffix if suffix else "")

    new_section = f"#### {category}:\n1. {entry}\n"
    if not block.strip():
        return "\n" + new_section + "\n"

    category_index = CATEGORIES.index(category)
    for later in CATEGORIES[category_index + 1 :]:
        later_match = re.search(rf"(?m)^####\s+{re.escape(later)}:\s*$", block)
        if later_match:
            return (
                block[: later_match.start()].rstrip()
                + "\n\n"
                + new_section
                + "\n"
                + block[later_match.start() :].lstrip()
            )
    return block.rstrip() + "\n\n" + new_section + "\n"


def check_repo(repo: Path) -> dict[str, Any]:
    findings: list[Finding] = []
    changelog = find_changelog(repo)

    if not changelog.exists():
        findings.append(
            Finding(
                "error",
                rel(repo, changelog),
                "missing ChangeLog/changeLog",
                "release or closeout flow must write changeLog before completion",
            )
        )
    else:
        text = read_text(changelog)
        if not re.search(r"(?m)^##\s+Unreleased\s*$", text):
            findings.append(
                Finding(
                    "error",
                    rel(repo, changelog),
                    "missing Unreleased section",
                    "issue closeout entries must land in Unreleased first",
                )
            )

    makefile = repo / "Makefile"
    if not makefile.exists():
        findings.append(Finding("warning", "Makefile", "missing Makefile"))
    else:
        make_text = read_text(makefile)
        for target in ("harness-check", "test", "build"):
            if not re.search(rf"(?m)^{target}:", make_text):
                findings.append(Finding("warning", "Makefile", f"missing target: {target}"))

    status = "ok"
    if any(item.severity == "error" for item in findings):
        status = "error"
    elif findings:
        status = "needs_attention"

    return {
        "repo": str(repo),
        "status": status,
        "changelog": rel(repo, changelog),
        "findings": [item.as_dict() for item in findings],
    }


def normalize_changed(paths: list[str]) -> list[str]:
    normalized: list[str] = []
    for item in paths:
        value = item.strip()
        if not value:
            continue
        if value.startswith("./"):
            value = value[2:]
        normalized.append(value)
    return normalized


def load_changed_files(paths: list[str], changed_files_from: str | None = None) -> list[str]:
    combined = list(paths)
    if changed_files_from:
        if changed_files_from == "-":
            data = sys.stdin.read()
        else:
            data = Path(changed_files_from).read_text(encoding="utf-8")
        combined.extend(data.splitlines())
    return normalize_changed(combined)


def has_changelog_change(paths: list[str]) -> bool:
    return any(
        p in CHANGELOG_CANDIDATES
        or p.endswith("/ChangeLog.md")
        or p.endswith("/changeLog.md")
        for p in paths
    )


def classify(paths: list[str], issue: str | None = None) -> dict[str, Any]:
    changed = normalize_changed(paths)
    joined = "\n".join(changed)
    classification = "issue-only"
    reasons: list[str] = []

    if has_changelog_change(changed):
        classification = "changelog-only"
        reasons.append("changeLog file changed")

    if any(p.startswith((".agents/", "docs/harness/", "docs/symphony/")) for p in changed):
        classification = "workflow-policy"
        reasons.append("harness/prompt/Symphony workflow policy changed")

    if re.search(r"^(cmd|internal)/|^go\.(mod|sum)$|^Makefile$", joined, re.MULTILINE):
        classification = "runtime-build"
        reasons.append("Go runtime/build surface changed")

    if any(p.startswith(("README.md", "docs/design/", "docs/release/")) for p in changed):
        if classification == "issue-only":
            classification = "release-docs"
        reasons.append("public docs or release notes changed")

    return {
        "issue": issue or "",
        "classification": classification,
        "changed_files": changed,
        "requires_changelog_before_done": True,
        "reasons": reasons or ["no release-sensitive files detected"],
    }


def changelog_gate(repo: Path, paths: list[str], issue: str | None = None) -> dict[str, Any]:
    changed = normalize_changed(paths)
    classification = classify(changed, issue)
    changelog = rel(repo, find_changelog(repo))

    if not changed:
        status = "skipped"
        message = "no changed files detected"
    elif has_changelog_change(changed):
        status = "pass"
        message = "changeLog file is present in changed files"
    else:
        status = "fail"
        message = "ChangeLog.md/changeLog.md is missing from changed files"

    return {
        "repo": str(repo),
        "status": status,
        "message": message,
        "issue": issue or "",
        "changelog": changelog,
        "changed_files": changed,
        "classification": classification["classification"],
        "requires_changelog_before_done": classification["requires_changelog_before_done"],
        "reasons": classification["reasons"],
    }


def changelog_add(repo: Path, issue: str, category: str, text: str, write: bool) -> dict[str, Any]:
    if category not in CATEGORIES:
        raise SystemExit(f"invalid type: {category}")
    path = find_changelog(repo)
    original = read_text(path) if path.exists() else ""
    updated = ensure_unreleased(original)
    bounds = find_unreleased_bounds(updated)
    if not bounds:
        raise SystemExit("failed to create Unreleased section")
    _, content_start, content_end = bounds
    entry = f"[{issue}] {text}" if issue else text
    block = updated[content_start:content_end]
    new_block = add_entry_to_block(block, category, entry)
    updated = updated[:content_start] + new_block + updated[content_end:]
    if write:
        write_text(path, updated)
    return {
        "path": str(path),
        "write": write,
        "changed": updated != original,
        "changelog_action": "updated Unreleased",
        "changelog_version": "Unreleased",
        "entry": entry,
    }


def release_archive(repo: Path, version: str, date: str, write: bool) -> dict[str, Any]:
    validate_semver(version)
    if not re.fullmatch(r"\d{8}", date):
        raise SystemExit("date must be YYYYMMDD")
    path = find_changelog(repo)
    original = read_text(path)
    bounds = find_unreleased_bounds(original)
    if not bounds:
        raise SystemExit("missing Unreleased section")
    header_start, content_start, content_end = bounds
    block = original[content_start:content_end].strip()
    if not block:
        raise SystemExit("Unreleased section is empty")
    release_block = f"### {version}({date})\n{block}\n\n"
    updated = original[:header_start] + "## Unreleased\n\n" + release_block + original[content_end:].lstrip("\n")
    if write:
        write_text(path, updated)
    return {
        "path": str(path),
        "write": write,
        "version": version,
        "date": date,
        "changed": updated != original,
        "changelog_action": f"archived {version}",
        "changelog_version": version,
    }


def run_self_test() -> dict[str, Any]:
    tmp = Path(tempfile.mkdtemp(prefix="symphony-go-version-skill-"))
    try:
        repo = tmp / "symphony-go"
        repo.mkdir()
        write_text(repo / "Makefile", "harness-check:\n\ntest:\n\nbuild:\n")
        before = check_repo(repo)
        add = changelog_add(repo, "SYM-1", "note", "增加 release skill 自测条目。", True)
        gate_fail = changelog_gate(repo, ["cmd/symphony/main.go"], "SYM-2")
        gate_pass = changelog_gate(repo, ["cmd/symphony/main.go", "ChangeLog.md"], "SYM-2")
        archive = release_archive(repo, "v0.1.0", "20260502", True)
        after = check_repo(repo)
        return {
            "status": "ok",
            "before_status": before["status"],
            "after_status": after["status"],
            "add": add,
            "gate_fail": gate_fail,
            "gate_pass": gate_pass,
            "archive": archive,
        }
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true", help="run built-in self-test")
    sub = parser.add_subparsers(dest="command")

    check = sub.add_parser("check")
    check.add_argument("--repo", required=True)
    check.add_argument("--json", action="store_true")

    classify_cmd = sub.add_parser("classify")
    classify_cmd.add_argument("--repo", required=True)
    classify_cmd.add_argument("--issue")
    classify_cmd.add_argument("--changed-files", nargs="*", default=[])
    classify_cmd.add_argument("--changed-files-from")
    classify_cmd.add_argument("--json", action="store_true")

    gate = sub.add_parser("changelog-gate")
    gate.add_argument("--repo", required=True)
    gate.add_argument("--issue")
    gate.add_argument("--changed-files", nargs="*", default=[])
    gate.add_argument("--changed-files-from")
    gate.add_argument("--json", action="store_true")

    add = sub.add_parser("changelog-add")
    add.add_argument("--repo", required=True)
    add.add_argument("--issue", default="")
    add.add_argument("--type", required=True, choices=CATEGORIES)
    add.add_argument("--text", required=True)
    add.add_argument("--write", action="store_true")
    add.add_argument("--json", action="store_true")

    archive = sub.add_parser("release-archive")
    archive.add_argument("--repo", required=True)
    archive.add_argument("--version", required=True)
    archive.add_argument("--date", required=True)
    archive.add_argument("--write", action="store_true")
    archive.add_argument("--json", action="store_true")

    return parser


def main(argv: list[str]) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.self_test:
        emit(run_self_test(), True)
        return 0
    if not args.command:
        parser.error("command is required unless --self-test is used")

    repo = ensure_repo(args.repo)
    if args.command == "check":
        emit(check_repo(repo), args.json)
    elif args.command == "classify":
        changed = load_changed_files(args.changed_files, args.changed_files_from)
        emit(classify(changed, args.issue), args.json)
    elif args.command == "changelog-gate":
        changed = load_changed_files(args.changed_files, args.changed_files_from)
        result = changelog_gate(repo, changed, args.issue)
        emit(result, args.json)
        if result["status"] == "fail":
            return 1
    elif args.command == "changelog-add":
        emit(changelog_add(repo, args.issue, args.type, args.text, args.write), args.json)
    elif args.command == "release-archive":
        emit(release_archive(repo, args.version, args.date, args.write), args.json)
    else:
        parser.error(f"unknown command: {args.command}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
