# Skill distribution via the installer

The repo ships a usage skill (`skills/browser-bridge`) that teaches LLM agents how to drive the bridge well, but end users installing via `curl | bash` had no working delivery path for it: `install.sh` had an opt-in `--with-skills` flag that downloads `browser-bridge-skills-<version>.tar.gz` from the GitHub release, yet no release workflow ever built that asset, so the flag failed against every real release. The flag's fallback was just as broken for users without a pre-existing Claude setup: default-target detection hard-failed (BB-E207) unless `~/.claude/skills` already existed. Result: the skill effectively reached nobody.

The delivery path is made real and default-on. A new `release-skills.yml` workflow (backed by `.github/scripts/build-skills-tarball.sh`) ships `browser-bridge-skills-<version>.tar.gz` + `.sha256` as release assets — the skill as a single top-level `browser-bridge/` directory, exactly the layout `install.sh`'s `download_skills` already expected. The installer now installs skills *by default*: without an explicit `--skills-dir` it installs into both `~/.agents/skills/` and `~/.claude/skills/`, but only into targets whose parent directory (`~/.agents` resp. `~/.claude`) already exists (`[[ -d ]]` follows symlinks deliberately, so symlinked config roots are honored); when neither parent exists it prints a note and skips. Upgrades use replace semantics (`rm -rf` the target's `browser-bridge/` before copying) so stale files from older skill versions cannot survive. The whole skills step is best-effort and never fails the overall install. `--no-skills`/`BB_NO_SKILLS` opt out; `--with-skills`/`BB_WITH_SKILLS` stay accepted as deprecated no-ops; `--skills-dir` overrides to a single explicit target.

## Considered Options

- **Keep skills opt-in (`--with-skills`)**: leaves the skill undiscovered by exactly the users it is written for — someone who just piped an installer into bash does not know a usage skill exists, and agents never see it either.
- **Fail hard when no default target exists** (the old BB-E207 behavior): the skill is auxiliary content; blocking or aborting a bridge install because the user has no agent config directory yet is backwards.
- **Bundle the skill into the runtime tarball** instead of a separate asset: couples skill content to the Go build and arch matrix for no benefit — the installer already had the separate-tarball contract, and a single arch-independent asset is cheaper to build and upload.

## Consequences

- **Cross-version behavior change**: `bridge [service] update` on older clients downloads the *latest* release's `install.sh` to upgrade itself, so existing installs will receive the skill on their next update without opting in. This is intentional (it is how the skill reaches the installed base) and the maintainer records it in the CHANGELOG.
- Default-on targets a moving asset: installing a *pinned* pre-skills version 404s on the skills tarball, which is why the skills step degrades to a warning instead of dying.
- The release checklist grows from 5 to 7 asset files (skills tarball + sha256), and a fourth workflow (`release-skills.yml`) must go green before a release is announced.
- `--with-skills`/`BB_WITH_SKILLS` remain accepted forever as no-ops: update scripts, docs, and user muscle memory already reference them, and removing them would break cross-version upgrade invocations for no gain.
- With two default targets the tarball is downloaded once per target; the asset is a few KB, so the duplication is accepted in exchange for keeping `download_skills` untouched.
