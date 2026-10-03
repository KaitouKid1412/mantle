# A PARITY.md excerpt for tests

## CLI: flags, subcommands, drift (owner 11)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CLI-01 | Flag table | Every flag classified | N | M1 | 11 | A | |

## Slash command index

| Command | Aliases | Type | Headless | Code | Row |
|---|---|---|---|---|---|
| `/clear` | reset, new | local | yes | E | SE-01 |

## Keybinding contexts index

| Context | Main actions (defaults) | Owner | Rows |
|---|---|---|---|
| Chat | enter submit | 04 | ED-01 |
| AbovePrompt / AbovePromptInput | tab focus | 07 | CH-01 |

## UI settings keys index

| Key | Purpose | Owner | Row |
|---|---|---|---|
| `statusLine` (`command`, `padding`) | | 07 | CH-18 |
| `copyFullResponse` (global), `diffTool` (global) | `/copy`, `/diff` | 06 | SE-29 |
| `spinnerTipsEnabled` | tips | 07 | CH-02 |

## CLI flags index (owner 11)

| Flag | Treatment | Notes |
|---|---|---|
| `-p, --print` | consumed | |
| `--output-format`, `--input-format` | -p only | |
