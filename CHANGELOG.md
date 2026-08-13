# Changelog

## [1.2.0](https://github.com/divmora/localharness/compare/v1.1.0...v1.2.0) (2026-08-13)


### Features

* implement GUI Spaces using sqlite and installation_id ([ad0d7fa](https://github.com/divmora/localharness/commit/ad0d7fa9fe462780a55e4b136ed57ea1bf57f936))
* implement live terminal streaming and improve SSH connection handling ([4969a86](https://github.com/divmora/localharness/commit/4969a869f7bb8675aeec90a13c6f3cccac6ab995))
* support multi-window architecture for isolated connections ([f80eaa3](https://github.com/divmora/localharness/commit/f80eaa3336a9cf78fa9b4421da72df69b286c45d))
* support multi-window architecture for isolated connections ([f67c603](https://github.com/divmora/localharness/commit/f67c603e5eba0bf94614d879acd0c27c04cdf75c))


### Bug Fixes

* keep sidecar process alive to prevent premature closure ([694b124](https://github.com/divmora/localharness/commit/694b124aef692921e8b1c7d5bbdb08fd281a6fb5))
* update TaskManager.StartBackground signature in system-messages example ([6cf13cf](https://github.com/divmora/localharness/commit/6cf13cfa491895461f06a2ae1f49dd7c9fba3858))

## [1.1.0](https://github.com/divmora/localharness/compare/v1.0.0...v1.1.0) (2026-08-13)


### Features

* add support for .agents/AGENTS.md rule file ([bf8471c](https://github.com/divmora/localharness/commit/bf8471c249b900db370f9ef632bf426f5cf5ac3e))
* **backend:** implement thin-client architecture and SSH tunneling ([432eeb1](https://github.com/divmora/localharness/commit/432eeb1864cca75dfab495256587e42448e30e4f))
* **backend:** use version from release-please manifest and allow optional ssh config ([beaea5d](https://github.com/divmora/localharness/commit/beaea5d8ab22bec238f6d0fa2b225bc6431f1265))
* **gui:** overhaul empty state to match Devin premium black layout ([3fa86c3](https://github.com/divmora/localharness/commit/3fa86c3e0c3bf5ca115c4ea6c4d47c280383d7c0))
* **gui:** phase 10 workspace empty state, file tabs, and file explorer ([a3bcbf1](https://github.com/divmora/localharness/commit/a3bcbf1b1c89e5c3c0c73669cb07e082f4cd1ae2))
* **gui:** phase 11 customizations modal and settings expansion ([40e4657](https://github.com/divmora/localharness/commit/40e46570ffa8a027873bf89fc30f7ac2f6b11965))
* **gui:** phase 9 session board, layout overhaul, and agent transparency ([fa80ae3](https://github.com/divmora/localharness/commit/fa80ae36d7ed23262380ddb2f05f7cfd67b0f585))
* implement session switching in dashboard ([21c3e36](https://github.com/divmora/localharness/commit/21c3e368203225dade3c5998312309a72cd0378a))
* implement UI scaffold and sidecar handshake ([fb39d12](https://github.com/divmora/localharness/commit/fb39d123a7d41456a11d6939691b68914f2aafea))
* refactor UI to 3-pane orchestration layout and wire terminal/sessions ([b3c7c3a](https://github.com/divmora/localharness/commit/b3c7c3a949cd5ce2ff3824f1cf13a3f905c2dfc0))
* scaffold Tauri GUI and GitHub Actions ([f1f745d](https://github.com/divmora/localharness/commit/f1f745d9635aa618d910d0530c5dd0d84b9bd5c5))
* **ui:** add remote-aware LLM configuration form to customizations modal ([567d72c](https://github.com/divmora/localharness/commit/567d72c06e911a1310f7df1310ec7df70f9a5b78))
* **ui:** implement dual-pane native ssh config selector and editor modal ([5962303](https://github.com/divmora/localharness/commit/59623035656ace509f24b04cdfc1cb6a11407065))
* **ui:** make file explorer and customizations fully connection-aware ([debd8c6](https://github.com/divmora/localharness/commit/debd8c61870d49315c71606ebed5525147b59209))
* **ui:** make session list and ssh connect visibility remote-aware ([f472116](https://github.com/divmora/localharness/commit/f47211693b11b2d8c6567880868c7d15b197dd93))
* **ui:** spawn new tauri window for secondary ssh connections ([306fd39](https://github.com/divmora/localharness/commit/306fd3989a2d7f8937fa5506b242f5a7592e914f))
* **ui:** support multiple LLM endpoints in Customizations Manager ([50f4740](https://github.com/divmora/localharness/commit/50f4740a81ac5413d98eafdb0b36dd9dbc609ac2))
* upgrade Editor and Terminal panels with tabbed Devin-style layout ([dd0caab](https://github.com/divmora/localharness/commit/dd0caab3948b427f2e79ab75e7d5be6edac168af))
* wire agent steps to Editor and Terminal panels ([b87cc9d](https://github.com/divmora/localharness/commit/b87cc9d03f82c4095716260bd5163a71ed0916a7))
* wire Browser and Planner tabs for agent actions ([57c446b](https://github.com/divmora/localharness/commit/57c446b39bf770ea1c3c827fe2a456fc46ccf5aa))
* wire up ChatPanel to useHarness and stream agent responses ([8c7b36f](https://github.com/divmora/localharness/commit/8c7b36f516d642367fc9246a05e16a78fc1445eb))


### Bug Fixes

* abstract syscalls for windows compatibility in task_manager ([746fe18](https://github.com/divmora/localharness/commit/746fe1836e2195b88c97fcf9a11c73a353cfb1f7))
* automate sidecar compilation for target architecture in tauri build ([57f49b3](https://github.com/divmora/localharness/commit/57f49b32073b4247307bb964fca643b52c40a7d6))
* **backend:** prevent SSH connection from hanging due to bash profile or script text polluting stdout ([cb1e077](https://github.com/divmora/localharness/commit/cb1e077ce5fb77b9e8a85f390b400bb2ba4b40c7))
* **backend:** ssh auto-deploy script now dynamically determines platform and includes version in filename ([f275baa](https://github.com/divmora/localharness/commit/f275baa2f372afd1fbd428a209041de9ad7b1562))
* **build:** remove deprecated make build-gui step from tauri.conf.json ([ea0d191](https://github.com/divmora/localharness/commit/ea0d191bc9e9db30bfe401e7b28d41e2f07fb48d))
* generate universal macOS sidecar using lipo ([aba0580](https://github.com/divmora/localharness/commit/aba0580ea4b32359d0df098a66c1ed623ebb1981))
* **gui:** always render WorkspacePanel even when no session is active ([1220bf4](https://github.com/divmora/localharness/commit/1220bf465b4cf8d1cc6d7f05ddc1f819f6ed6cb8))
* **gui:** resolve ipv6 loopback mismatch causing GUI disconnect ([a6377d5](https://github.com/divmora/localharness/commit/a6377d5515960a5e7ffa4a0acc2531e1bc9c2a85))
* **gui:** resolve typescript errors in gui build ([266d6a7](https://github.com/divmora/localharness/commit/266d6a7f55ddc805bd54ad31d94b240c3a009afa))
* **gui:** wire up new session buttons to generate random UUID ([9f8617a](https://github.com/divmora/localharness/commit/9f8617a3091beaaf152a18a5756f38b1f7a11d93))
* makefile build-gui target missing .exe on windows ([41efa07](https://github.com/divmora/localharness/commit/41efa07f6daf86e8920950b37721f663f8157383))
* resolve WebSocket connection drops and React render crashes in GUI ([99e85e2](https://github.com/divmora/localharness/commit/99e85e234d0d6b957c218d891d398f9ced8a0b99))
* revert error_event rename in ServerMessage ([cefcea0](https://github.com/divmora/localharness/commit/cefcea018f2a49d743f5eb9b14af59b5f335769e))
* **ui:** change default LLM endpoint fallback from pixelvide-cloud to divmora ([4821657](https://github.com/divmora/localharness/commit/4821657d9ca167b90bcaa6f23400287ba3661509))
* update react-resizable-panels to v4 syntax ([fdff9be](https://github.com/divmora/localharness/commit/fdff9be06f04915db5c2b90df6932ad14b3b1443))

## 1.0.0 (2026-08-11)


### Features

* initial commit ([eb93354](https://github.com/divmora/localharness/commit/eb9335440bd8a863637e5bc21f6958bf13e031a7))

## Changelog
