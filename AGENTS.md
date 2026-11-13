# Engineering conventions

- Preserve the documented Codex and DeepSeek Harness original-session relay behavior. Keep identity, origin, permissions, idempotency and recovery explicit.
- Keep the project independently installable. Do not depend on BuddyBio source paths, credentials, services or user-global profiles.
- Keep secrets, personal conversations and one-off verification evidence outside Git. Report execution results in chat; documentation contains repeatable procedures and supported boundaries.
- Each durable value has one owning module. Coordinate protocol, schema and generated client changes across their owners.
- Keep native task approvals. Do not enable global Full Access or widen home-directory permissions to bypass setup problems.
- Verify meaningful behavior using real internal persistence and boundary substitutes where appropriate; distinguish offline tests from real host and Feishu checks.
- After complete implementation and self-checks, independent acceptance uses gpt-6.1-terra/high; subsequent independent code review uses gpt-6-astra/medium. Implementation agents use gpt-6.1-sol/medium.
- Do not overwrite other agents' edits or modify existing BuddyBio installations. Update and uninstall only resources owned by this installation.
- Do not add explanatory code comments or docstrings. Express intent through names, structure, tests and documentation; retain machine-semantic directives.
