from __future__ import annotations

import signed_artifact_evidence


DEFAULT_VERSION = "<version+build>"
DEFAULT_SCOPE = "android-ios"
DEFAULT_TEAM_ID = "<APPLE_TEAM_ID>"
DEFAULT_SIGNING_TEAM_ID = "<REAL_APPLE_TEAM_ID>"
DEFAULT_EXPORT_METHOD = "<export-method>"
DEFAULT_QA_RECORDS_DIR = "docs/qa-builds"
DEFAULT_IOS_ARCHIVE_OR_BUILD = "build/ios/archive/MaClawMobile.xcarchive"
VALID_SCOPES = ("android", "ios", "android-ios")
VALID_ANDROID_RELEASE_ARTIFACTS = ("apk", "appbundle")
AUTOMATED_RELEASE_GATE_COUNT = 38
AUTOMATED_RELEASE_GATE_SUCCESS_LINE = (
    f"All MaClaw Mobile automated release gates passed: {AUTOMATED_RELEASE_GATE_COUNT} gates passed."
)
QA_RELEASE_EVIDENCE_LINK_COMMAND = (
    "python3 tool/qa_release_evidence_links.py "
    "docs/qa-builds --update-release-evidence"
)


def _is_placeholder(value: str) -> bool:
    normalized = value.strip().lower()
    return normalized.startswith("<") and normalized.endswith(">")


def _version_parts(version: str) -> tuple[str, str]:
    if "+" not in version:
        raise ValueError("version must use <app-version>+<build-number>, for example 1.0.0+42")
    app_version, build_number = version.split("+", 1)
    if not app_version or not build_number:
        raise ValueError("version must include both app version and build number")
    return app_version, build_number


def validate_scope(scope: str) -> str:
    if scope not in VALID_SCOPES:
        raise ValueError(
            f"unsupported scope: {scope}; expected one of {', '.join(VALID_SCOPES)}",
        )
    return scope


def scope_label(scope: str) -> str:
    scope = validate_scope(scope)
    return {
        "android": "Android",
        "ios": "iOS",
        "android-ios": "Android/iOS",
    }[scope]


def scope_covers_android(scope: str | None) -> bool:
    return scope in {"android", "android-ios"}


def scope_covers_ios(scope: str | None) -> bool:
    return scope in {"ios", "android-ios"}


def final_decision_prefills(
    version: str = DEFAULT_VERSION,
    *,
    scope: str = DEFAULT_SCOPE,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> dict[str, str]:
    scope = validate_scope(scope)
    return {
        "Release handoff result": (
            "release_handoff.py output saved to "
            f"{handoff_evidence_path(version, scope=scope, records_dir=records_dir)}"
        ),
        "Runtime boundary verification result": (
            "MaClaw Mobile runtime boundary verified. "
            f"log: {records_dir}/runtime-boundary-{version}.log"
        ),
        "Automated release gates result": (
            f"run_release_gates.py: {AUTOMATED_RELEASE_GATE_COUNT} gates passed; "
            f"log: {records_dir}/release-gates-{version}.log"
        ),
    }


def create_record_command(
    *,
    scope: str = DEFAULT_SCOPE,
    version: str = DEFAULT_VERSION,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    prefills = final_decision_prefills(version, scope=scope, records_dir=records_dir)
    command = (
        f"python3 tool/create_qa_build_record.py --scope {scope} --version {version} "
        f'--release-handoff-result "{prefills["Release handoff result"]}" '
        f'--runtime-boundary-result "{prefills["Runtime boundary verification result"]}" '
        f'--automated-gates-result "{prefills["Automated release gates result"]}"'
    )
    if records_dir != DEFAULT_QA_RECORDS_DIR:
        command += f" --records-dir {records_dir}"
    return command


def release_status_report_command(
    *,
    scope: str = DEFAULT_SCOPE,
    team_id: str | None = DEFAULT_TEAM_ID,
    export_method: str | None = DEFAULT_EXPORT_METHOD,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    command = f"python3 tool/release_status_report.py --scope {scope}"
    if scope_covers_ios(scope):
        command += f" --team-id {team_id or DEFAULT_TEAM_ID}"
        command += f" --export-method {export_method or DEFAULT_EXPORT_METHOD}"
    if records_dir != DEFAULT_QA_RECORDS_DIR:
        command += f" --records-dir {records_dir}"
    return command


def release_handoff_command(
    *,
    version: str = DEFAULT_VERSION,
    scope: str = DEFAULT_SCOPE,
    team_id: str | None = DEFAULT_TEAM_ID,
    export_method: str | None = DEFAULT_EXPORT_METHOD,
    output: str | None = None,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    output = output or handoff_evidence_path(
        version,
        scope=scope,
        records_dir=records_dir,
    )
    command = f"python3 tool/release_handoff.py --version {version} --scope {scope}"
    if scope_covers_ios(scope):
        command += f" --team-id {team_id or DEFAULT_TEAM_ID}"
        command += f" --export-method {export_method or DEFAULT_EXPORT_METHOD}"
    if records_dir != DEFAULT_QA_RECORDS_DIR:
        command += f" --records-dir {records_dir}"
    command += f" --output {output}"
    return command


def qa_preflight_command(
    *,
    scope: str = DEFAULT_SCOPE,
    team_id: str | None = None,
    export_method: str | None = None,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    command = f"python3 tool/qa_preflight.py --scope {scope}"
    if team_id:
        command += f" --team-id {team_id}"
    if export_method:
        command += f" --export-method {export_method}"
    if records_dir != DEFAULT_QA_RECORDS_DIR:
        command += f" --records-dir {records_dir}"
    return command


def handoff_evidence_path(
    version: str = DEFAULT_VERSION,
    *,
    scope: str = DEFAULT_SCOPE,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    if scope != DEFAULT_SCOPE:
        return f"{records_dir}/handoff-{scope}-{version}.md"
    return f"{records_dir}/handoff-{version}.md"


def qa_record_path_placeholder(
    *,
    scope: str = DEFAULT_SCOPE,
    version: str = DEFAULT_VERSION,
    date: str = "<YYYY-MM-DD>",
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    return f"{records_dir}/{date}-{scope}-{version}.md"


def runtime_boundary_command(
    version: str = DEFAULT_VERSION,
    *,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    return (
        "python3 tool/verify_runtime_boundary.py --log "
        f"{records_dir}/runtime-boundary-{version}.log"
    )


def release_gates_command(
    version: str = DEFAULT_VERSION,
    *,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    return (
        "python3 tool/run_release_gates.py --log "
        f"{records_dir}/release-gates-{version}.log"
    )


def final_release_evidence_log_path(
    version: str = DEFAULT_VERSION,
    *,
    scope: str = DEFAULT_SCOPE,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    if scope != DEFAULT_SCOPE:
        return f"{records_dir}/final-release-evidence-{scope}-{version}.log"
    return f"{records_dir}/final-release-evidence-{version}.log"


def setup_android_signing_command() -> str:
    return "python3 tool/setup_android_signing.py"


def setup_ios_export_options_command(
    *,
    team_id: str = DEFAULT_SIGNING_TEAM_ID,
    export_method: str = DEFAULT_EXPORT_METHOD,
) -> str:
    return (
        f"python3 tool/setup_ios_export_options.py --team-id {team_id} "
        f"--export-method {export_method}"
    )


def ios_release_plan_command(
    *,
    team_id: str = DEFAULT_SIGNING_TEAM_ID,
    export_method: str = DEFAULT_EXPORT_METHOD,
    provisioning_profiles: str | None = None,
    record_dir: str | None = None,
) -> str:
    if (provisioning_profiles is None) != (record_dir is None):
        raise ValueError(
            "iOS QA artifact evidence command requires provisioning_profiles "
            "and record_dir together",
        )
    return (
        f"python3 tool/plan_ios_release.py --team-id {team_id} "
        f"--export-method {export_method}"
        + (
            f' --provisioning-profiles "{provisioning_profiles}" '
            f"--record-dir {record_dir}"
            if provisioning_profiles is not None and record_dir is not None
            else ""
        )
    )


def android_release_build_command(
    version: str = DEFAULT_VERSION,
    *,
    artifact: str = "apk",
    dry_run: bool = False,
    record_dir: str | None = None,
    signing_identity: str | None = None,
    installer_channel: str | None = None,
) -> str:
    if artifact not in VALID_ANDROID_RELEASE_ARTIFACTS:
        raise ValueError(
            "unsupported Android release artifact: "
            f"{artifact}; expected one of {', '.join(VALID_ANDROID_RELEASE_ARTIFACTS)}",
        )
    evidence_options = [record_dir, signing_identity, installer_channel]
    if any(option is not None for option in evidence_options) and not all(evidence_options):
        raise ValueError(
            "Android QA artifact evidence command requires record_dir, "
            "signing_identity, and installer_channel together",
        )
    app_version, build_number = _version_parts(version)
    command = (
        f"python3 tool/build_android_release.py --artifact {artifact} "
        f"--build-name {app_version} --build-number {build_number}"
    )
    if dry_run:
        command += " --dry-run"
    if record_dir is not None:
        command += (
            f" --record-dir {record_dir} "
            f'--signing-identity "{signing_identity}" '
            f'--installer-channel "{installer_channel}"'
        )
    return command


def android_artifact_evidence_command(
    version: str = DEFAULT_VERSION,
    *,
    artifact: str = "<signed-release.apk-or-aab>",
    record_dir: str = DEFAULT_QA_RECORDS_DIR,
    signing_identity: str = "<alias or certificate fingerprint>",
    installer_channel: str = "<internal test channel>",
) -> str:
    if not _is_placeholder(artifact) and not signed_artifact_evidence.is_trackable_android_artifact(
        artifact,
    ):
        raise ValueError(
            "Android artifact evidence command requires a signed/release/internal "
            ".apk or .aab artifact path that does not contain debug.",
        )
    if not _is_placeholder(signing_identity) and not signed_artifact_evidence.is_android_signing_identity(
        signing_identity,
    ):
        raise ValueError(
            "Android artifact evidence command requires a non-debug release/internal signing identity.",
        )
    if not _is_placeholder(installer_channel) and not signed_artifact_evidence.is_installer_channel(
        installer_channel,
    ):
        raise ValueError(
            "Android artifact evidence command requires a non-debug auditable installer channel.",
        )
    return (
        f"python3 tool/signed_artifact_evidence.py android {artifact} "
        f"--record-dir {record_dir} --version {version} "
        f'--signing-identity "{signing_identity}" '
        f'--installer-channel "{installer_channel}"'
    )


def ios_artifact_evidence_command(
    *,
    archive_or_build: str = DEFAULT_IOS_ARCHIVE_OR_BUILD,
    team_id: str = DEFAULT_SIGNING_TEAM_ID,
    provisioning_profiles: str = "<Runner profile UUID/name; Share Extension profile UUID/name>",
    record_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    if not _is_placeholder(archive_or_build) and not signed_artifact_evidence.is_trackable_ios_archive(
        archive_or_build,
    ):
        raise ValueError(
            "iOS artifact evidence command requires an .xcarchive path or explicit TestFlight build number.",
        )
    if not _is_placeholder(team_id) and signed_artifact_evidence.APPLE_TEAM_ID_RE.fullmatch(
        team_id.strip().upper(),
    ) is None:
        raise ValueError(
            "iOS artifact evidence command requires a 10-character Apple Team ID.",
        )
    if not _is_placeholder(provisioning_profiles) and not signed_artifact_evidence.is_trackable_ios_profiles(
        provisioning_profiles,
    ):
        raise ValueError(
            "iOS artifact evidence command requires Runner and Share Extension provisioning profile UUID/file/name evidence.",
        )
    return (
        "python3 tool/signed_artifact_evidence.py ios "
        f'--archive-or-build "{archive_or_build}" '
        f"--team-id {team_id} "
        f'--provisioning-profiles "{provisioning_profiles}" '
        f"--record-dir {record_dir}"
    )


def validate_qa_build_record_command(record: str = qa_record_path_placeholder()) -> str:
    return f"python3 tool/validate_qa_build_record.py {record}"


def qa_build_record_report_command(record: str = qa_record_path_placeholder()) -> str:
    return f"python3 tool/qa_build_record_report.py {record}"


def validate_qa_build_records_dir_command(
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
    *,
    scope: str = DEFAULT_SCOPE,
) -> str:
    scope = validate_scope(scope)
    command = f"python3 tool/validate_qa_build_records_dir.py {records_dir}"
    if scope != DEFAULT_SCOPE:
        command += f" --scope {scope}"
    return command


def qa_release_evidence_link_command(
    *,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
    scope: str = DEFAULT_SCOPE,
) -> str:
    scope = validate_scope(scope)
    command = f"python3 tool/qa_release_evidence_links.py {records_dir} --update-release-evidence"
    if scope != DEFAULT_SCOPE:
        command += f" --scope {scope}"
    return command


def verify_final_release_evidence_command(
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
    *,
    scope: str = DEFAULT_SCOPE,
    version: str | None = None,
    log: str | None = None,
) -> str:
    scope = validate_scope(scope)
    command = f"python3 tool/verify_final_release_evidence.py {records_dir} --scope {scope}"
    log_path = log
    if log_path is None and version is not None:
        log_path = final_release_evidence_log_path(
            version,
            scope=scope,
            records_dir=records_dir,
        )
    if log_path:
        command += f" --log {log_path}"
    return command


def signed_qa_record_hint(
    *,
    scope: str = DEFAULT_SCOPE,
    version: str = DEFAULT_VERSION,
    team_id: str = DEFAULT_TEAM_ID,
    signing_team_id: str | None = None,
    export_method: str = DEFAULT_EXPORT_METHOD,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    scope = validate_scope(scope)
    signing_team_id = signing_team_id or (
        DEFAULT_SIGNING_TEAM_ID if team_id == DEFAULT_TEAM_ID else team_id
    )
    record = qa_record_path_placeholder(
        scope=scope,
        version=version,
        records_dir=records_dir,
    )
    setup_hints = []
    if scope_covers_android(scope):
        setup_hints.append(f"run `{setup_android_signing_command()}`")
    if scope_covers_ios(scope):
        setup_hints.append(
            f"run `{setup_ios_export_options_command(team_id=signing_team_id, export_method=export_method)}`"
        )
    artifact_hints = []
    if scope_covers_android(scope):
        artifact_hints.append(
            "generate Android artifact evidence with "
            f"`{android_artifact_evidence_command(version, record_dir=records_dir)}`"
        )
    if scope_covers_ios(scope):
        ios_plan_command = ios_release_plan_command(
            team_id=signing_team_id,
            export_method=export_method,
        )
        ios_plan_evidence_command = ios_release_plan_command(
            team_id=signing_team_id,
            export_method=export_method,
            provisioning_profiles="<Runner profile UUID/name; Share Extension profile UUID/name>",
            record_dir=records_dir,
        )
        ios_evidence_command = ios_artifact_evidence_command(
            team_id=signing_team_id,
            record_dir=records_dir,
        )
        artifact_hints.append(
            "plan iOS archive/export first with "
            f"`{ios_plan_command}`; after the signed .xcarchive/TestFlight build "
            "exists, generate iOS artifact evidence with "
            f"`{ios_plan_evidence_command}` or "
            f"`{ios_evidence_command}`",
        )
    artifact_hint = ""
    if artifact_hints:
        artifact_hint = "; " + "; ".join(artifact_hints)
    preflight_team_id = team_id if scope_covers_ios(scope) else None
    preflight_export_method = export_method if scope_covers_ios(scope) else None
    return (
        "no completed signed-build QA records yet; release handoff is only a "
        "QA plan, not a completed QA record; run "
        f"`{release_handoff_command(version=version, scope=scope, team_id=team_id, export_method=export_method, records_dir=records_dir)}`; "
        + "; ".join(setup_hints)
        + "; "
        f"then run `{qa_preflight_command(scope=scope, team_id=preflight_team_id, export_method=preflight_export_method, records_dir=records_dir)}`; "
        "then capture "
        f"`{runtime_boundary_command(version, records_dir=records_dir)}` "
        "and "
        f"`{release_gates_command(version, records_dir=records_dir)}`; "
        f"create the record with `{create_record_command(scope=scope, version=version, records_dir=records_dir)}`"
        f"{artifact_hint}; "
        "after completing evidence validate it with "
        f"`{validate_qa_build_record_command(record)}`; "
        "if validation fails inspect gaps with "
        f"`{qa_build_record_report_command(record)}`; "
        "after records validate run "
        f"`{qa_release_evidence_link_command(scope=scope, records_dir=records_dir)}`; "
        "then verify final evidence with "
        f"`{verify_final_release_evidence_command(records_dir, scope=scope, version=version, log=final_release_evidence_log_path(version, scope=scope, records_dir=records_dir))}`"
    )


def qa_release_evidence_link_hint(
    *,
    scope: str = DEFAULT_SCOPE,
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
    version: str = DEFAULT_VERSION,
) -> str:
    scope = validate_scope(scope)
    return (
        f"run `{qa_release_evidence_link_command(scope=scope, records_dir=records_dir)}` to write validated links "
        "into docs/release_evidence.md, then run "
        f"`{verify_final_release_evidence_command(records_dir, scope=scope, version=version, log=final_release_evidence_log_path(version, scope=scope, records_dir=records_dir))}`"
    )


def qa_build_record_report_hint(record: str = "<record.md>") -> str:
    return (
        f"run `{qa_build_record_report_command(record)}` to inspect "
        "missing or invalid signed-build QA evidence"
    )


def qa_record_version_mismatch_hint(
    records_dir: str = DEFAULT_QA_RECORDS_DIR,
) -> str:
    return (
        "keep final release QA records to one version/build; move records for "
        f"other builds out of {records_dir} or regenerate them with the same "
        "`--version <version+build>` before updating release evidence links"
    )
