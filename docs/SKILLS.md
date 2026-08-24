# MRSS Skills

English | [简体中文](SKILLS.zh.md)

MRSS ships a Codex skill package for users who want an AI agent to inspect and operate their MRSS data through the local REST API.

## What Is Included

The release asset `MRSS-<version>-skills.zip` contains:

- `mrss-assistant/SKILL.md` - agent workflow and safety rules.
- `mrss-assistant/references/api.md` - generated API reference from `docs/SERVER_MODE/swagger.json`.
- `mrss-assistant/scripts/generate_api_reference.py` - maintenance script for regenerating the API reference.
- `mrss-assistant/agents/openai.yaml` - Codex UI metadata.

## Install

1. Download `MRSS-<version>-skills.zip` from the GitHub release page.
2. Extract it.
3. Copy the extracted `mrss-assistant` folder into your Codex skills directory:

Windows:

```powershell
Copy-Item -Recurse .\mrss-assistant "$env:USERPROFILE\.codex\skills\"
```

macOS or Linux:

```bash
cp -R ./mrss-assistant ~/.codex/skills/
```

4. Restart Codex so it can discover the new skill.

## Use

Start the MRSS desktop app. Released desktop builds expose `http://127.0.0.1:1234/api` on the loopback interface while MRSS is running. The API is not exposed to other computers on the network. If port `1234` is occupied, MRSS continues running but the local API remains unavailable until the port is freed and the app is restarted.

Alternatively, run MRSS in headless server mode:

```bash
docker run -p 1234:1234 ghcr.io/marcomarcogd/mrss:latest
```

Then ask Codex:

```text
Use $mrss-assistant to inspect my unread MRSS articles and summarize the most important items.
```

The skill defaults to `http://127.0.0.1:1234/api`. If your instance uses another host or port, include it in the prompt.

## Safety Model

The skill tells agents to:

- prefer read-only API calls first;
- ask before destructive operations, bulk status changes, cache clearing, or settings updates;
- redact credentials and API keys from responses;
- use MRSS API endpoints instead of direct SQLite access unless the user explicitly asks for offline inspection.

## Maintainers

When API routes change, regenerate the reference:

```bash
python skills/mrss-assistant/scripts/generate_api_reference.py docs/SERVER_MODE/swagger.json skills/mrss-assistant/references/api.md
```

CI checks that the generated reference stays in sync. The release workflow packages the skill as `MRSS-<version>-skills.zip` and uploads it as a release asset.
