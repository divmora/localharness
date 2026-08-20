# Changelog

## [2.0.0](https://github.com/divmora/localharness/compare/v1.4.0...v2.0.0) (2026-08-20)


### ⚠ BREAKING CHANGES

* drop economy and budget, add manual agent hiring system

### Features

* add daemon, tui, and replay support ([b7154df](https://github.com/divmora/localharness/commit/b7154df8bf3afa4698cfddaa82ed50759290629b))
* add interactive question cards, custom slash commands, subagent handoff briefings, and background tasks monitoring ([0e676aa](https://github.com/divmora/localharness/commit/0e676aa8992472b6ee1d6d19dd74a329ab2fdad2))
* drop economy and budget, add manual agent hiring system ([26729c7](https://github.com/divmora/localharness/commit/26729c78460c3ee68ed42a242561eaef3b3decba))
* improve GUI UX with draggable top bar, React logs, and sorted sessions ([051cbf9](https://github.com/divmora/localharness/commit/051cbf9b30d7ad1506f2543121c3a6b0e9c48135))
* segregate output logs by process with dropdown selector ([0620763](https://github.com/divmora/localharness/commit/0620763f7bd75f58add2f6558e7ea06f88529da6))
* use fully dynamic pixel-to-percentage conversion for panels ([5dca5e5](https://github.com/divmora/localharness/commit/5dca5e573b9bd23c8028b3b70f9395c3ef851c78))


### Bug Fixes

* change system prompt examples to use native file tools ([3f17354](https://github.com/divmora/localharness/commit/3f17354a47e5af3cee2a0601b1dbaeb16ae955be))
* dynamically filter office managers against active sessions ([4385511](https://github.com/divmora/localharness/commit/43855114088251f1f23d6e5eb383e93e34c97b6e))
* **gui:** comprehensive UI improvements, bug fixes, and test updates ([0e8f570](https://github.com/divmora/localharness/commit/0e8f5704bf917291c5233efe741a064f8bb09339))
* increase hit area for panel resize handles ([20b1d33](https://github.com/divmora/localharness/commit/20b1d33b36a1defd4d9bc93ebb775d17cba84576))
* prevent infinite recursion in TauriLogger ([a915756](https://github.com/divmora/localharness/commit/a91575671da65c6fb93871b90be407a7acc335a8))
* refactor tauri commands to prevent macro collision and fix dynamic workspace ([4607deb](https://github.com/divmora/localharness/commit/4607debb85b35487075129baa8c7c8a0ebf80393))
* resolve workspace selection, session deletion, and chat history order bugs ([66d1d62](https://github.com/divmora/localharness/commit/66d1d62693853a49a2b86fb09c34f884b9b4d7af))
* terminal panel layout and message send routing ([f50dbb2](https://github.com/divmora/localharness/commit/f50dbb2362cef031c9263b6c9b59f7db7529dc96))

## [1.4.0](https://github.com/divmora/localharness/compare/v1.3.0...v1.4.0) (2026-08-18)


### Features

* add dynamic agent hiring, cross-agent messaging, and dedicated Office Manager UI ([57c701d](https://github.com/divmora/localharness/commit/57c701d303f9520179580069dbc6f3eed9c04d3a))
* add terminal toggle icon to topbar in chat mode ([9bf338a](https://github.com/divmora/localharness/commit/9bf338a4109dbccff312d262c30b8b24ed604cee))
* **gui:** add UI state persistence, fix trajectory state input blocking ([e5947bd](https://github.com/divmora/localharness/commit/e5947bd79bbb2af2381015d82951e4876f652772))
* implement agent capacity scaling, E2E framework, and test fixtures ([785b476](https://github.com/divmora/localharness/commit/785b476ccef4331fe59fa96954b18048171ad2fc))
* migrate localharness to tauri event-based architecture managed by rust proxy ([cfd1e01](https://github.com/divmora/localharness/commit/cfd1e012c43a5b3aa6b9451afc68e4ff31039ffe))
* require archiving sessions before they can be deleted ([99f9393](https://github.com/divmora/localharness/commit/99f93931c08f9ea23bdc1049e5035b8a3040a50d))
* use backend-generated session IDs for new harnesses ([4651ba6](https://github.com/divmora/localharness/commit/4651ba6ab4b4b11ec76d723118ad643eb6d4938b))
* validate llm endpoint name to only allow lowercase alphanumeric and internal hyphens ([783cf80](https://github.com/divmora/localharness/commit/783cf80f275b6a4e0f6d6f99e5ea21e4418455d1))


### Bug Fixes

* add delete session and save sidebar sizing ([402f193](https://github.com/divmora/localharness/commit/402f193b339a296097e3d5dabd95a40802703d20))
* await websocket proxy connection during session startup to prevent Disconnected state ([c8fb559](https://github.com/divmora/localharness/commit/c8fb5594291c68eb5f5c51b004fc16436eaa42fc))
* change budget exhausted error to use DC ([c177d19](https://github.com/divmora/localharness/commit/c177d1999a50b614c4fea79ee06ad5d17b298eb1))
* explicitly add Connection and Upgrade headers to websocket requests ([aae6a26](https://github.com/divmora/localharness/commit/aae6a26264cf9c324c51819ede362e4aad4d833b))
* filter sessions by active office and optimize e2e test caching ([f603d2c](https://github.com/divmora/localharness/commit/f603d2c33885a05da531ae02ce5beebadad4307c))
* keep sidecar stdin open to prevent immediate EOF shutdown ([bda2644](https://github.com/divmora/localharness/commit/bda2644b9fb48c17173b51481e30eaba0fc0e630))
* periodically flush WebSocket sink to send keepalive pongs ([a226125](https://github.com/divmora/localharness/commit/a22612544a1ab6f3dc6871b4e3dee34f0f080dc9))
* prevent delete llm endpoint from improperly reopening edit form ([9a552fa](https://github.com/divmora/localharness/commit/9a552fa6af03d94c53fb1e9716ed791ac6f95190))
* prevent deletion of default llm endpoint ([1dc1e17](https://github.com/divmora/localharness/commit/1dc1e1760f56af217d7fc50030578a7f620270e9))
* prevent deletion of default llm endpoint in backend ([2d4b3bf](https://github.com/divmora/localharness/commit/2d4b3bfa6c27255176dd86abd5373631adf4179b))
* prevent terminal state clear on toggle by hiding instead of unmounting ([05b28da](https://github.com/divmora/localharness/commit/05b28dab39607e4d49289a5133ed104233978c36))
* rename payload to message in send_harness_message tauri command ([1dc21ee](https://github.com/divmora/localharness/commit/1dc21ee39cd474915ead26efbb3991a1b8da4adf))
* replace window.prompt with custom react state for adding llm endpoints ([0c5671b](https://github.com/divmora/localharness/commit/0c5671beec1533a7a494182486e6ebe61ba7f9fb))
* resolve ghost session deletion and infinite office websocket reconnection ([35cf379](https://github.com/divmora/localharness/commit/35cf3798f4a09a6d4a3cc71dd73c835a73da483a))
* resolve react async state bug in llm endpoint deletion and defaulting ([3b3c72d](https://github.com/divmora/localharness/commit/3b3c72dc33457ccfbb251e6839b98bc240bd8788))
* resolve xterm rendering overlap by removing container padding ([5f9182e](https://github.com/divmora/localharness/commit/5f9182e590f1bf6f1a94050327579b5af7b946fc))
* use into_client_request to generate required WebSocket handshake headers (Sec-WebSocket-Key) ([94e7757](https://github.com/divmora/localharness/commit/94e77576d7dad397e630043c44eda7d340e0e036))

## [1.3.0](https://github.com/divmora/localharness/compare/v1.2.0...v1.3.0) (2026-08-17)


### Features

* add 2D isometric office view foundation ([52e54ea](https://github.com/divmora/localharness/commit/52e54ea2c3132f07cc74f8d43f4febe319ffc517))
* add command palette and theme switcher with db persistence ([47d3c62](https://github.com/divmora/localharness/commit/47d3c62f5e7e95c0be8df53332bf3a43c5146854))
* add generic command palette with live theme preview and remove sidebar toggle ([dfbeb4a](https://github.com/divmora/localharness/commit/dfbeb4a64b6b9c79a40b6a2ec0ab286743c11bce))
* Add interruption API (pause/resume) with UI integration ([61ecf6f](https://github.com/divmora/localharness/commit/61ecf6f96f23309549083aa34c5bf837865cc892))
* add structured metadata to ErrorEvent protobuf and wire up HarnessError ([4c7e9dc](https://github.com/divmora/localharness/commit/4c7e9dc90e85b9050cdb7bc62d0f8b31983db099))
* add system tray, desktop notifications, and fix window spawning ([457801a](https://github.com/divmora/localharness/commit/457801a6f1d36309638ec35341ed054788c13796))
* add workspace selection dropdown with recent workspaces and fix native folder dialog ([9a2d54f](https://github.com/divmora/localharness/commit/9a2d54f92358c88625910839691f9a155f43b14e))
* **adk:** add ErrorCode and ErrorMetadata to Step and ToolError ([abc48d8](https://github.com/divmora/localharness/commit/abc48d802160a269a22095a1b5372aa9d361db62))
* agent budget allocation via manager UI ([21038c1](https://github.com/divmora/localharness/commit/21038c1fbfc8f8e7ea7920003ca2ffaec9ca3ce9))
* **gui:** add Toast component and update UI utilities ([3b089c1](https://github.com/divmora/localharness/commit/3b089c19ad488e04a8690b644769321d2b00050b))
* **gui:** combine chat and workspace panes, stream sidecar stderr logs to terminal ([735590b](https://github.com/divmora/localharness/commit/735590bb992726c0e8d2a620c3c37c54b9919d71))
* **gui:** run app in background by hiding window on close ([479ad11](https://github.com/divmora/localharness/commit/479ad1196ce650ef888776da21f9cf9fec74e01b))
* implement 4-pane resizable layout and rename AgentSidebar ([95dbd67](https://github.com/divmora/localharness/commit/95dbd672f94bda32502980b6b1762d20c2e08c0e))
* implement frameless custom top toolbar layout ([97e8d94](https://github.com/divmora/localharness/commit/97e8d946383aef0579aa3d171e706f1ffbaac6ed))
* implement persistent recent projects modal and backend updates ([e014a4e](https://github.com/divmora/localharness/commit/e014a4eef7d1c41490d95938a55b9039cc95882f))
* implement SessionsManager UI with board and list views ([80bdcf5](https://github.com/divmora/localharness/commit/80bdcf59f8d20e080448048b83b49ba6cc2e1cac))
* map live sessions and add state animations to office view ([cbb9476](https://github.com/divmora/localharness/commit/cbb9476c2d9e3b17b425ef62a376015e07bf6392))
* redesign chat panel UI and remove new window button ([a2284a9](https://github.com/divmora/localharness/commit/a2284a9a8edc01950c93780df9f2181f5875190e))
* redesign LLM config UI to list view ([51d1812](https://github.com/divmora/localharness/commit/51d18121e74b0c61a958c31102370ee5168cb60a))
* refactor UI into page components and strictly scope session visibility ([568c1ea](https://github.com/divmora/localharness/commit/568c1ea44d96d2d5c22fcb1a5cde2750064c14ea))
* UI improvements for chat mode and manager budget controls ([5f8e1c8](https://github.com/divmora/localharness/commit/5f8e1c8702c49de2405338ed9ef3301037e1aed1))


### Bug Fixes

* **core:** queue early user messages and flush on init ([977cd97](https://github.com/divmora/localharness/commit/977cd976ef31a7205242ff8408543f973d3b13f5))
* correctly parse source field from transcript jsonl ([fa995a1](https://github.com/divmora/localharness/commit/fa995a1a82115bc93422defe939055797f1316a9))
* ensure Browse button is always visible in ProjectSelectionModal ([84f981c](https://github.com/divmora/localharness/commit/84f981c658a1b7aacd90b84407aab2fc1b4a5d48))
* ensure selecting a session resets view to main chat ([fe07d9c](https://github.com/divmora/localharness/commit/fe07d9c8f8fa6fda46d134ccf831d001c2040649))
* **gui:** change localhost to 127.0.0.1 and improve connection logging ([2f775b2](https://github.com/divmora/localharness/commit/2f775b2560f0ff0204473f9b3dd7671abe66d54b))
* **gui:** fix duplicate chat messages and improve user message contrast ([0231200](https://github.com/divmora/localharness/commit/0231200a87b94a4f3c52485c014fd2fa95c4deb4))
* implement message queue for initial WebSocket payloads to prevent drop ([09a5c26](https://github.com/divmora/localharness/commit/09a5c264b6265d64a58088981c868842090854a4))
* keep ssh port forwarding process alive in tauri backend ([adc8874](https://github.com/divmora/localharness/commit/adc8874d2c8a9e5459524b57517bd38c39428f92))
* lazy load localharness engine and surface connection errors ([385d5a3](https://github.com/divmora/localharness/commit/385d5a397da873b93b4ffc491c75368e645ed3b7))
* load chat history on reload and resolve text duplication in chat bubbles ([e3003d9](https://github.com/divmora/localharness/commit/e3003d9bb493baaaf5bd3072e8397113bc614758))
* remove unused React import causing build failure ([1ce0c72](https://github.com/divmora/localharness/commit/1ce0c720996b08382f3dc1e9ae5c5fc44b8b2342))
* reorder TopBar icons and hide traffic light space on fullscreen ([33e2937](https://github.com/divmora/localharness/commit/33e29377e5e75f6f980cd326923bbbbc5e87b21d))
* **ssh:** add StrictHostKeyChecking=accept-new and BatchMode=yes to prevent hangs ([f6a0b7b](https://github.com/divmora/localharness/commit/f6a0b7b494100bd12580f6656bff788e98a07e51))

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
