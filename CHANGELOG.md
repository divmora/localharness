# Changelog

## [0.4.1](https://github.com/divmora/localharness/compare/v0.4.0...v0.4.1) (2026-09-24)


### Bug Fixes

* prevent infinite permission retry loops, detached session stalls, and MCP process leaks ([611f664](https://github.com/divmora/localharness/commit/611f66436fd268824aef6315db216b60acba4a92))


### Performance Improvements

* **engine,cli:** multi-turn context bloat optimization and adaptive browser activation ([#72](https://github.com/divmora/localharness/issues/72)) ([36c60c5](https://github.com/divmora/localharness/commit/36c60c50577b9d132373ed6c66c3ab1768e92d21))

## [0.4.0](https://github.com/divmora/localharness/compare/v0.3.0...v0.4.0) (2026-09-23)


### Features

* add universal desktop automation, browser enhancements, and redesigned tui ([85f91bf](https://github.com/divmora/localharness/commit/85f91bf167c918789a203ab4ddeee4488ee90b68))
* add version command and flags to lhctl ([57a3e6d](https://github.com/divmora/localharness/commit/57a3e6d65e346fc0d230b9023a12ed9b3e3ae6f1))
* **cli:** litellm endpoint integration and management ([#70](https://github.com/divmora/localharness/issues/70)) ([1e201bd](https://github.com/divmora/localharness/commit/1e201bd583531e47aa8d6de200c495c342bffa32))
* **core:** dynamic runtime model switching (/model &lt;name&gt;) ([#69](https://github.com/divmora/localharness/issues/69)) ([c512b5b](https://github.com/divmora/localharness/commit/c512b5be6cb695de334150f0914fb3e44616db45))
* **core:** general-purpose agent access modes ([#42](https://github.com/divmora/localharness/issues/42)) ([d1edb3b](https://github.com/divmora/localharness/commit/d1edb3bc25c642ea5a7cd2955f372e12cb53c746))
* integrate code-reviewer-ai-agent as agents/code-reviewer submodule ([9025522](https://github.com/divmora/localharness/commit/902552233703769bb9807bf37fad17eb1ee71ca8))
* **lhctl:** add voice dictation, spoken responses, and workspace directory commands ([6fa3a31](https://github.com/divmora/localharness/commit/6fa3a31af468d5eaec9df205f7e431a591c092d6))
* **lhctl:** improve copy, multiline paste, and console history retention in TUI ([1bc7940](https://github.com/divmora/localharness/commit/1bc7940feec4541c48fd577b8d48033b19a9bb7a))
* **lhctl:** transition TUI to AGY-style inline terminal with native scrollback ([267d928](https://github.com/divmora/localharness/commit/267d928dfb1fcc3e7f556b5d218272682e3fb256))


### Bug Fixes

* **cli,tui,engine:** fix startup log spam, version banner, prompt caching, tui resumption, and inspect content ([#71](https://github.com/divmora/localharness/issues/71)) ([7c9dabc](https://github.com/divmora/localharness/commit/7c9dabc985d1b973cee3552441c2e13809ad011b))
* **conversation:** eliminate lock inversion deadlock in Flush and step writer ([#51](https://github.com/divmora/localharness/issues/51)) ([678d22c](https://github.com/divmora/localharness/commit/678d22c499da96cae2fbcaf9cc5cba8e0c898c2e))
* **engine:** clone tool registry per engine to prevent subagent step emitter race condition ([53f03e0](https://github.com/divmora/localharness/commit/53f03e037bf81e4c23e2503c24954a1d6b1feb7e))
* **engine:** deduplicate turn token usage across tool executions ([#46](https://github.com/divmora/localharness/issues/46)) ([fafe6df](https://github.com/divmora/localharness/commit/fafe6dfd150bd71a6daee38f25b7ba2891223f38))
* **engine:** pass isolated subagent environment variables without mutating global state ([#53](https://github.com/divmora/localharness/issues/53)) ([04644fc](https://github.com/divmora/localharness/commit/04644fcf31c51729e829a0bc2f04c7598f04a43b))
* **engine:** thread-safe lazy initialization for desktop driver ([#52](https://github.com/divmora/localharness/issues/52)) ([c44888f](https://github.com/divmora/localharness/commit/c44888fa453fb6d2063302c47acab6651d5b2491))
* **lhctl:** remove extra blank lines between consecutive tool calls and items ([6ef3d73](https://github.com/divmora/localharness/commit/6ef3d73d1a8b6b5718d6e80c67874f82996f6c69))
* **llm:** sort streaming tool call indices to preserve execution order ([#48](https://github.com/divmora/localharness/issues/48)) ([bc688a2](https://github.com/divmora/localharness/commit/bc688a2793fe8549578ea4c47b64288aa9875845))
* route /btw side questions through LiteLLM config instead of direct Gemini/OpenAI keys ([faee345](https://github.com/divmora/localharness/commit/faee345c570068fb52c706d1b5dd7ab70b0ac54a))
* **server:** isolate turn cancellation to current turn context ([#43](https://github.com/divmora/localharness/issues/43)) ([7671520](https://github.com/divmora/localharness/commit/7671520bbf0402029da525b74bd0551c7d9ce132))
* **server:** prevent cleanup hang on client disconnect during questions and permissions ([#44](https://github.com/divmora/localharness/issues/44)) ([d3abf81](https://github.com/divmora/localharness/commit/d3abf817c6753b40e289d2787eb56b36a12cefa6))
* **tools:** apply multi-chunk edits bottom-to-top with line delta adjustments ([#45](https://github.com/divmora/localharness/issues/45)) ([a246f46](https://github.com/divmora/localharness/commit/a246f463e30c113ad45cb96bed4d7f2dbc555147))
* **tools:** fix file descriptor leaks and stream ripgrep output ([#37](https://github.com/divmora/localharness/issues/37)) ([2c888bf](https://github.com/divmora/localharness/commit/2c888bf8905d6226033dc512dd2fca063233caec))
* **tools:** handle context cancellation during task wait and default cwd to workspace ([#47](https://github.com/divmora/localharness/issues/47)) ([033d4ab](https://github.com/divmora/localharness/commit/033d4ab4eda060973741497912736b2f2fba3c51))
* **tools:** parse Windows paths with drive letters in ripgrep search ([#49](https://github.com/divmora/localharness/issues/49)) ([8cdf5d1](https://github.com/divmora/localharness/commit/8cdf5d10d2f08fb6e635d96a3202389f76a7c3c9))
* **tools:** protect read_url_content against SSRF, private IPs, and cloud metadata ([#54](https://github.com/divmora/localharness/issues/54)) ([74b4aec](https://github.com/divmora/localharness/commit/74b4aec4a6ae69cda6fe06fdcb4a06b8d816e6d4))
* **tools:** recover persistent terminal on command timeout or cancellation ([#50](https://github.com/divmora/localharness/issues/50)) ([44acbee](https://github.com/divmora/localharness/commit/44acbee2ca2e7a92987bd59befe5550e14d433e4))
* **tools:** use NUL byte detection and extension matching for view_file ([#55](https://github.com/divmora/localharness/issues/55)) ([8a40156](https://github.com/divmora/localharness/commit/8a40156d22cb4cb9b2f5fac09716728b8db28b1f))


### Performance Improvements

* **codegraph:** batch sqlite transactions and mtime incremental cache ([#56](https://github.com/divmora/localharness/issues/56)) ([1bc58f3](https://github.com/divmora/localharness/commit/1bc58f3f10525c435d00bd126e7dd2aea0b9f2d3))
* **codegraph:** eliminate full BM25 FTS re-indexing on every query and linear call hierarchy scans ([#33](https://github.com/divmora/localharness/issues/33)) ([3c6760f](https://github.com/divmora/localharness/commit/3c6760f1e460011c5cb455be6ec3e98085be1b29))
* **codegraph:** parallel AST parsing with bounded worker pool ([#61](https://github.com/divmora/localharness/issues/61)) ([edd3cea](https://github.com/divmora/localharness/commit/edd3ceac2e23251dc44963f5dedf3a644393ac0f))
* **codegraph:** zero-allocation camelCase tokenizer and direct term weighting in FTS index ([#62](https://github.com/divmora/localharness/issues/62)) ([ac172aa](https://github.com/divmora/localharness/commit/ac172aa55c061d405e3393cf142024bd400858a9))
* **conversation:** asynchronous buffered transcript logging ([#58](https://github.com/divmora/localharness/issues/58)) ([a21c7ef](https://github.com/divmora/localharness/commit/a21c7efb66c4818f1bc29612bbe2b75b0db19f74))
* **diff:** replace O(M*N) 2D dynamic programming in UnifiedDiff with linear Myers and patience diff ([#32](https://github.com/divmora/localharness/issues/32)) ([ef451bd](https://github.com/divmora/localharness/commit/ef451bd00695574de559a0793de4ec311a1677dd))
* **engine:** implement concurrent parallel execution for read-only tools ([#35](https://github.com/divmora/localharness/issues/35)) ([d5f0963](https://github.com/divmora/localharness/commit/d5f096396eebdd51f78ea2f71f683ce7b79321d7))
* **engine:** zero-allocation line counting and slicing in context reduction ([#67](https://github.com/divmora/localharness/issues/67)) ([c455ab8](https://github.com/divmora/localharness/commit/c455ab8643f7b73d1e48fbf2cca172406b739154))
* **engine:** zero-allocation tool call argument token estimation ([#68](https://github.com/divmora/localharness/issues/68)) ([1ab41ec](https://github.com/divmora/localharness/commit/1ab41ecf86725046a2e4eb5d25b5e7a6438010ef))
* **server:** outbound websocket write pump and buffer pooling ([#59](https://github.com/divmora/localharness/issues/59)) ([5a7770d](https://github.com/divmora/localharness/commit/5a7770d9ba7d2ad40315357dc1984645ebf75049))
* **session:** asynchronously persist step content via write-behind worker ([#40](https://github.com/divmora/localharness/issues/40)) ([7730afc](https://github.com/divmora/localharness/commit/7730afc4239382522e156178ff744d77b6e8ee2f))
* **streaming:** batch and debounce streaming chunk updates ([#39](https://github.com/divmora/localharness/issues/39)) ([b9ea1ff](https://github.com/divmora/localharness/commit/b9ea1ff699f0e1b54b240266254b522e85a24054))
* **subagent:** support model tiering resolution and compress subagent handoff prompt injection ([#41](https://github.com/divmora/localharness/issues/41)) ([2ddf4ef](https://github.com/divmora/localharness/commit/2ddf4ef81a5a39a0c377211d2a1e23d2e3c711bd))
* **task_manager:** replace busy-wait marker polling with event-driven notifications ([#38](https://github.com/divmora/localharness/issues/38)) ([3e90432](https://github.com/divmora/localharness/commit/3e90432c9954c6841adcde25ba988d929db77fec))
* **tools:** concurrent and non-allocating directory child counting ([#63](https://github.com/divmora/localharness/issues/63)) ([d9e3ff2](https://github.com/divmora/localharness/commit/d9e3ff22d10f850a7827c2f8f4bb4904f3c07bef))
* **tools:** early-exit streaming and fd integration in find_file ([#65](https://github.com/divmora/localharness/issues/65)) ([287120e](https://github.com/divmora/localharness/commit/287120e8641f050706d6884b189c19a6c23149b4))
* **tools:** eliminate recursive walk in list_dir, optimize view_file streaming and replace_file_content byte scanning ([#36](https://github.com/divmora/localharness/issues/36)) ([4ac68a2](https://github.com/divmora/localharness/commit/4ac68a245288baecf69f6499d2d5acf728f0afd7))
* **tools:** zero-allocation ASCII fold scanning and reflection-free line parsing ([#66](https://github.com/divmora/localharness/issues/66)) ([8e27cb1](https://github.com/divmora/localharness/commit/8e27cb1cf08f64bfdccc6d5105460b0608222997))
* **tools:** zero-allocation byte scanning and early walk termination ([#60](https://github.com/divmora/localharness/issues/60)) ([a265d4d](https://github.com/divmora/localharness/commit/a265d4dd6e3e41b187d5bba1acc39bb549938f5b))
* **util:** hunk-scoped unified diff with context collapsing ([#57](https://github.com/divmora/localharness/issues/57)) ([a2950c6](https://github.com/divmora/localharness/commit/a2950c69e731236138a37945f6dd418053c9557f))
* **workspace:** cache canonical roots to eliminate redundant EvalSymlinks syscalls ([#64](https://github.com/divmora/localharness/issues/64)) ([c736e01](https://github.com/divmora/localharness/commit/c736e015e44013663065ec63c19d0ad863f54275))

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
