# Changelog

## [0.4.0](https://github.com/divmora/localharness/compare/v0.3.0...v0.4.0) (2026-09-12)


### Features

* add universal desktop automation, browser enhancements, and redesigned tui ([85f91bf](https://github.com/divmora/localharness/commit/85f91bf167c918789a203ab4ddeee4488ee90b68))
* add version command and flags to lhctl ([57a3e6d](https://github.com/divmora/localharness/commit/57a3e6d65e346fc0d230b9023a12ed9b3e3ae6f1))
* integrate code-reviewer-ai-agent as agents/code-reviewer submodule ([9025522](https://github.com/divmora/localharness/commit/902552233703769bb9807bf37fad17eb1ee71ca8))
* **lhctl:** add voice dictation, spoken responses, and workspace directory commands ([6fa3a31](https://github.com/divmora/localharness/commit/6fa3a31af468d5eaec9df205f7e431a591c092d6))
* **lhctl:** improve copy, multiline paste, and console history retention in TUI ([1bc7940](https://github.com/divmora/localharness/commit/1bc7940feec4541c48fd577b8d48033b19a9bb7a))
* **lhctl:** transition TUI to AGY-style inline terminal with native scrollback ([267d928](https://github.com/divmora/localharness/commit/267d928dfb1fcc3e7f556b5d218272682e3fb256))


### Bug Fixes

* **lhctl:** remove extra blank lines between consecutive tool calls and items ([6ef3d73](https://github.com/divmora/localharness/commit/6ef3d73d1a8b6b5718d6e80c67874f82996f6c69))
* route /btw side questions through LiteLLM config instead of direct Gemini/OpenAI keys ([faee345](https://github.com/divmora/localharness/commit/faee345c570068fb52c706d1b5dd7ab70b0ac54a))

## [0.3.0](https://github.com/divmora/localharness/compare/v0.2.1...v0.3.0) (2026-09-09)


### Features

* add /context, /btw, artifact review card, and context reduction ([58db642](https://github.com/divmora/localharness/commit/58db642d5a9383a6a86bd9fd66d01c669628ad50))


### Bug Fixes

* allow lhctl to self-host daemon when localharness binary is not in PATH ([cb1990e](https://github.com/divmora/localharness/commit/cb1990edb19b1b3abdbc30a84a304211c41f5993))

## [0.2.1](https://github.com/divmora/localharness/compare/v0.2.0...v0.2.1) (2026-09-09)


### Bug Fixes

* support Windows binary resolution and daemon liveness in lhctl ([04e75c3](https://github.com/divmora/localharness/commit/04e75c3d6c2b2f479d56c71ddf739ac7cb0b6583))

## [0.2.0](https://github.com/divmora/localharness/compare/v0.1.0...v0.2.0) (2026-09-08)


### Features

* introduce localharness universal multi-agent orchestration harness ([78f066e](https://github.com/divmora/localharness/commit/78f066ee847809fd3418a322460b0de40bf72958))
