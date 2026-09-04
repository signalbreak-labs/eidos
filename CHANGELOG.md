# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.9.0](https://github.com/signalbreak-labs/eidos/compare/v0.8.0...v0.9.0) (2026-09-04)


### Features

* **generator:** TLS skip-verify, provider docs rework, seconds-based timeouts ([0d6fe2f](https://github.com/signalbreak-labs/eidos/commit/0d6fe2f4192a283eed5ffe9012d4ae6e36952394))
* **generator:** wire JSON-bodied ephemeral Opens + warn when an ephemeral stays unwired ([85ac737](https://github.com/signalbreak-labs/eidos/commit/85ac7375634d48dc16c3e4d58bd9ff5e6a100f22))


### Bug Fixes

* re-track examples/mycloud-provider/go.mod as the nested module boundary ([ed417a4](https://github.com/signalbreak-labs/eidos/commit/ed417a42c640cf5f752231fcc808bc1d004c3c17))


### Miscellaneous Chores

* **examples:** regenerate mycloud-provider sample with new provider schema/docs ([795f907](https://github.com/signalbreak-labs/eidos/commit/795f907a02ce8a11cbb1b097d2b94c28880ae5af))
* ignore go.sum produced by go mod tidy in examples/mycloud-provider ([b4135c8](https://github.com/signalbreak-labs/eidos/commit/b4135c84013d86dcc7abe9345c072ed1617f6c25))
* untrack examples/mycloud-provider/go.mod as well as go.sum ([bacd230](https://github.com/signalbreak-labs/eidos/commit/bacd2302d390bfbfc9325464b81239417766546d))

## [0.8.0](https://github.com/signalbreak-labs/eidos/compare/v0.7.4...v0.8.0) (2026-09-03)


### Features

* **generator:** child-resource read via read_collection_path + exclude_attributes ([01dd7a9](https://github.com/signalbreak-labs/eidos/commit/01dd7a9769d24b4c13abf131368a5677adb12403))


### Bug Fixes

* **generator:** child-resource read correctness + coverage for read_collection_path/exclude_attributes ([a66a0b3](https://github.com/signalbreak-labs/eidos/commit/a66a0b3ce83cf189f62e9a66190e7d60b0af1f03))

## [0.7.4](https://github.com/signalbreak-labs/eidos/compare/v0.7.3...v0.7.4) (2026-09-01)


### Bug Fixes

* **generator:** ship list import pairing + include_create_response_attributes/path_params overrides ([#63](https://github.com/signalbreak-labs/eidos/issues/63)) ([5eef830](https://github.com/signalbreak-labs/eidos/commit/5eef830b89494b06d7e605a37521ecc2d50212ba))

## [0.7.3](https://github.com/signalbreak-labs/eidos/compare/v0.7.2...v0.7.3) (2026-09-01)


### Bug Fixes

* **generator:** config_file schema, create descriptions, docs note, list names ([#61](https://github.com/signalbreak-labs/eidos/issues/61)) ([6450e08](https://github.com/signalbreak-labs/eidos/commit/6450e08e32c4183504472ba017c95b110c09c4ca))

## [0.7.2](https://github.com/signalbreak-labs/eidos/compare/v0.7.1...v0.7.2) (2026-08-31)


### Bug Fixes

* **generator:** gigavuecore regenerate-and-release CI failures ([#59](https://github.com/signalbreak-labs/eidos/issues/59)) ([1b6aa18](https://github.com/signalbreak-labs/eidos/commit/1b6aa18a4f487d7e7b206fb4ae500e0f9385c286))

## [0.7.1](https://github.com/signalbreak-labs/eidos/compare/v0.7.0...v0.7.1) (2026-08-31)


### Bug Fixes

* **crud:** rename colliding CRUD groups instead of dropping the loser ([3e64b0a](https://github.com/signalbreak-labs/eidos/commit/3e64b0adf964c6c4ec653d52863517caf39f1181))
* **crud:** warn when a sibling PUT shadows and consumes a PATCH operation ([f20c969](https://github.com/signalbreak-labs/eidos/commit/f20c969c518a4ec56c2a1182447f17d3f54cd1df))
* **diagnostics:** demote inert spec-keyword and additionalProperties:true diagnostics to Info ([68599d7](https://github.com/signalbreak-labs/eidos/commit/68599d704c811bd36edb664c03991373745c6f62))
* **docs:** carry the honest-scaffold invariant into docs and state TF versions ([95915b9](https://github.com/signalbreak-labs/eidos/commit/95915b9a6f96dba110868fdbd3b824aa4b61ee53))
* **generator:** bare function names, registry namespace key, docs/example cosmetics ([889ff6d](https://github.com/signalbreak-labs/eidos/commit/889ff6debb9c74c247a910f6e32d8ba59b885b5d))
* **generator:** emit plan modifiers and force replacement when Update is unwired ([2d478db](https://github.com/signalbreak-labs/eidos/commit/2d478dbc3418f218e7c1d5714c94fd5b0d0fd001))
* **generator:** make generator.yaml emission a lossless round-trip ([6a2d509](https://github.com/signalbreak-labs/eidos/commit/6a2d5095bd796601754f1cca0dd1de02e4b0a101))
* **lists:** pair list resources with managed resources so terraform query can register them ([87e53f4](https://github.com/signalbreak-labs/eidos/commit/87e53f469f3c9ac3f9d5f60417efa2605e43d7f3))
* **parser,api:** type normalization, singleton imports, secret docs notes ([6181f0c](https://github.com/signalbreak-labs/eidos/commit/6181f0c1951a7bf2bdba4213dc95995930b38023))


### Miscellaneous Chores

* **examples:** regenerate sample mycloud provider from audit fixes ([a7513b5](https://github.com/signalbreak-labs/eidos/commit/a7513b501aee8debfc9de8bce21be0ae4e780864))

## [0.7.0](https://github.com/signalbreak-labs/eidos/compare/v0.6.0...v0.7.0) (2026-08-31)


### Features

* **generator:** support nested identities ([#54](https://github.com/signalbreak-labs/eidos/issues/54)) ([39a8341](https://github.com/signalbreak-labs/eidos/commit/39a8341bc34713cb98a9002d9f3a09d45ef3333d)), closes [#53](https://github.com/signalbreak-labs/eidos/issues/53)


### Bug Fixes

* **descriptions:** carry parameter schema-level and list-identity descriptions through to generated schemas ([0e58afb](https://github.com/signalbreak-labs/eidos/commit/0e58afbedb725a2cadda233a98f0cdfb203c9eaf))
* **generator:** derive config placeholders from schema constraints so generated configs satisfy generated validators ([#52](https://github.com/signalbreak-labs/eidos/issues/52)) ([a90417b](https://github.com/signalbreak-labs/eidos/commit/a90417be9f4401d48ca6549f660e3a156eb53824))

## [0.6.0](https://github.com/signalbreak-labs/eidos/compare/v0.5.5...v0.6.0) (2026-08-29)


### Features

* **parser:** resolve local multi-file refs ([#49](https://github.com/signalbreak-labs/eidos/issues/49)) ([08f836c](https://github.com/signalbreak-labs/eidos/commit/08f836c54b05b88be495d8dfc156c4e22c3dad1b))

## [0.5.5](https://github.com/signalbreak-labs/eidos/compare/v0.5.4...v0.5.5) (2026-08-29)


### Bug Fixes

* action/ephemeral object+map schemas, computed-ID imports, populated examples ([#46](https://github.com/signalbreak-labs/eidos/issues/46)) ([6bc8725](https://github.com/signalbreak-labs/eidos/commit/6bc8725b29e65c2abfb5328631f0e742d9de9308))

## [0.5.4](https://github.com/signalbreak-labs/eidos/compare/v0.5.3...v0.5.4) (2026-08-28)


### Bug Fixes

* resolve deep-audit findings and expand test coverage ([a558c72](https://github.com/signalbreak-labs/eidos/commit/a558c72860b11ad600e2c104e5f4365f3cc16363))
* resolve golangci-lint findings (64 issues across 9 linters) ([7b22d03](https://github.com/signalbreak-labs/eidos/commit/7b22d03fa3af450b4b6ae32eccee8f00624991a6))

## [0.5.3](https://github.com/signalbreak-labs/eidos/compare/v0.5.2...v0.5.3) (2026-08-26)


### Bug Fixes

* correct acceptance-test generation against real-world schemas ([#41](https://github.com/signalbreak-labs/eidos/issues/41)) ([b13663d](https://github.com/signalbreak-labs/eidos/commit/b13663d33a9f2c477df727daabc51e55b66ff420))
* **transformer:** keep required query params on schema ([#43](https://github.com/signalbreak-labs/eidos/issues/43)) ([7039542](https://github.com/signalbreak-labs/eidos/commit/7039542fa3378ab3ae9f954c64bb6b13ef7a523c))

## [0.5.2](https://github.com/signalbreak-labs/eidos/compare/v0.5.1...v0.5.2) (2026-08-25)


### Miscellaneous Chores

* rendered examples ([#38](https://github.com/signalbreak-labs/eidos/issues/38)) ([f501585](https://github.com/signalbreak-labs/eidos/commit/f5015852e7ad53ad3dd1cf8c5eae36834863193b))

## [0.5.1](https://github.com/signalbreak-labs/eidos/compare/v0.5.0...v0.5.1) (2026-08-25)


### Bug Fixes

* **generator:** fmt-clean HCL examples, docs nested-schema sections, wire-name ID lookup in acceptance test mocks, zip archives + buildvcs/GPG release fixes, config-driven dynamic-release workflow ([b03063a](https://github.com/signalbreak-labs/eidos/commit/b03063a70e5c5a88db7ed6140103b1f5b9508311))


### Miscellaneous Chores

* update sample provider ([db7350e](https://github.com/signalbreak-labs/eidos/commit/db7350ee471aea4de1c7a048d74ad3d0a3bf145d))

## [0.5.0](https://github.com/signalbreak-labs/eidos/compare/v0.4.4...v0.5.0) (2026-08-20)


### Features

* **transformer:** carry OpenAPI descriptions onto generated attributes ([0c15d09](https://github.com/signalbreak-labs/eidos/commit/0c15d0953617fcb6aaf0e8442db052fa13f6de89)), closes [#28](https://github.com/signalbreak-labs/eidos/issues/28)
* **transformer:** fall back to the request body for attribute descriptions ([0fc5668](https://github.com/signalbreak-labs/eidos/commit/0fc5668abb1791877a5092cdb36a7085b6cc48ae)), closes [#28](https://github.com/signalbreak-labs/eidos/issues/28)

## [0.4.4](https://github.com/signalbreak-labs/eidos/compare/v0.4.3...v0.4.4) (2026-08-18)


### Bug Fixes

* mcp issues ([3b3194f](https://github.com/signalbreak-labs/eidos/commit/3b3194f22c90fee56028119548af655db0a08a31))

## [0.4.3](https://github.com/signalbreak-labs/eidos/compare/v0.4.2...v0.4.3) (2026-08-18)


### Bug Fixes

* heavy duty audit cleanup ([a75ea74](https://github.com/signalbreak-labs/eidos/commit/a75ea74c94bc50e316a1edee0e3762b9d444b6cf))
* **mcp:** resolve config file/URL, full-provider emit, only-build+dynamic ([89c9a16](https://github.com/signalbreak-labs/eidos/commit/89c9a167ce562f23f2d1c7578cc98577580e36db))

## [0.4.2](https://github.com/signalbreak-labs/eidos/compare/v0.4.1...v0.4.2) (2026-08-17)


### Bug Fixes

* bug fixes and mcp changes ([42bce6b](https://github.com/signalbreak-labs/eidos/commit/42bce6b73432010108736df4fb067f301ae0095a))

## [0.4.1](https://github.com/signalbreak-labs/eidos/compare/v0.4.0...v0.4.1) (2026-08-16)


### Bug Fixes

* mcp issues and generated assets ([4d6fea3](https://github.com/signalbreak-labs/eidos/commit/4d6fea37e2caf6774526c9f9160058a257ee8cb7))

## [0.4.0](https://github.com/signalbreak-labs/eidos/compare/v0.3.3...v0.4.0) (2026-08-16)


### Features

* treat PUT as create by default; accept spec refs in MCP tools ([329253d](https://github.com/signalbreak-labs/eidos/commit/329253d7a4e7e9d39b862bcf1d8a90e0fb9c71cd))


### Miscellaneous Chores

* add opt-in dynamic regenerate-and-release workflow ([f07971e](https://github.com/signalbreak-labs/eidos/commit/f07971e5a496d9cb8632153fadca532af3c61c9b))

## [0.3.3](https://github.com/signalbreak-labs/eidos/compare/v0.3.2...v0.3.3) (2026-08-16)


### Bug Fixes

* live testing cleanup and improve generated provider codecoverage ([895d9df](https://github.com/signalbreak-labs/eidos/commit/895d9dfb8443f4324fcb85fcc70ff89d28221cab))


### Miscellaneous Chores

* cleanup example ([327351d](https://github.com/signalbreak-labs/eidos/commit/327351d0c8c99637e25f588460788ba8118bf7ab))

## [0.3.2](https://github.com/signalbreak-labs/eidos/compare/v0.3.1...v0.3.2) (2026-08-14)


### Bug Fixes

* ephemeral and provider updates ([6b09e05](https://github.com/signalbreak-labs/eidos/commit/6b09e0593979131e4a343af5948b5fa57fe13ad9))
* failing ci checks ([c6bd8e3](https://github.com/signalbreak-labs/eidos/commit/c6bd8e3ab20a63ab2be0dabf51c881ae8a130a17))
* generation testing fixes ([08bcd46](https://github.com/signalbreak-labs/eidos/commit/08bcd46cb70c521e3a97796a5906e3d1267be548))
* many changes and corrections ([84b74db](https://github.com/signalbreak-labs/eidos/commit/84b74dbeff911ede3d89b1979b52f64a54237eb2))
* vulncheck ci ([2ac90c5](https://github.com/signalbreak-labs/eidos/commit/2ac90c5c40a1d98086b10aa8de9929d71cbfe2ac))


### Miscellaneous Chores

* updates from real provider generations ([ce7c2af](https://github.com/signalbreak-labs/eidos/commit/ce7c2afe1e45e523fde1a72ec5a749a20c555726))

## [0.3.1](https://github.com/signalbreak-labs/eidos/compare/v0.3.0...v0.3.1) (2026-08-09)


### Bug Fixes

* corrections from codecov improvements ([4bab24b](https://github.com/signalbreak-labs/eidos/commit/4bab24beeb3b8b1a21e4e8c2d0236791888b4601))


### Miscellaneous Chores

* add codeowners ([32ac87b](https://github.com/signalbreak-labs/eidos/commit/32ac87b75fc29d01d21b227f7871433a213dad34))
* bump ci versions ([b5e1ac6](https://github.com/signalbreak-labs/eidos/commit/b5e1ac662f2b15c2d1b03fe45223fde2efb99e29))
* bump ci versions ([9ba1ddb](https://github.com/signalbreak-labs/eidos/commit/9ba1ddb3391a5cdef5f1bcd65be549a95614a050))
* ci cleanup ([b550386](https://github.com/signalbreak-labs/eidos/commit/b5503864c1a2b0a78ceeb5331626acdf657a024f))
* improve codecov by adding tests ([3d30a5d](https://github.com/signalbreak-labs/eidos/commit/3d30a5d3fe1dc1226192e3ff727270738b4c5572))
* update codecov ([6ec7be6](https://github.com/signalbreak-labs/eidos/commit/6ec7be673a92c743583247de55e833d37b71c2b4))
* update docs for accuracy ([af73f1a](https://github.com/signalbreak-labs/eidos/commit/af73f1a67bc6abf63e2c7a4cfc4efcbd1d0d77fd))

## [0.3.0](https://github.com/signalbreak-labs/eidos/compare/v0.2.0...v0.3.0) (2026-08-08)


### Features

* initial commit ([5e831de](https://github.com/signalbreak-labs/eidos/commit/5e831de4c87a03a7d6bf7e3726b374306b153bee))


### Bug Fixes

* update ci ([662ec4a](https://github.com/signalbreak-labs/eidos/commit/662ec4a9ad033d6eb54b304105e1fd923c6c9424))
* update ci for brew ([4a0eb17](https://github.com/signalbreak-labs/eidos/commit/4a0eb17e40f6728e30ba440bf7a49dbaacf9bdab))
* update ci to release ([2879d28](https://github.com/signalbreak-labs/eidos/commit/2879d289caec5e342a9b1a15e70a6329b4fd40e2))
* update releaser ([904520f](https://github.com/signalbreak-labs/eidos/commit/904520f806e0079e8078b3ef5d4c2007951117bd))


### Miscellaneous Chores

* **main:** release eidos 0.2.0 ([5ea3e0a](https://github.com/signalbreak-labs/eidos/commit/5ea3e0acdc7db47b09ecc0b8b852834e5d8eb182))
* **main:** release eidos 0.2.0 ([75fda5f](https://github.com/signalbreak-labs/eidos/commit/75fda5f3938428d2d78e06db12d2e6e0558fe7ea))
* remove hardcoded release ([3eddef2](https://github.com/signalbreak-labs/eidos/commit/3eddef23e30578cf4ce7d91c103d3c02fad405d6))


### Documentation

* fix inaccuracies and formatting ([ade480b](https://github.com/signalbreak-labs/eidos/commit/ade480b5f383333e98842fc92ceb93fdd88ce3ee))

## [0.2.0](https://github.com/signalbreak-labs/eidos/compare/eidos-v0.1.0...eidos-v0.2.0) (2026-08-08)


### Features

* initial commit ([5e831de](https://github.com/signalbreak-labs/eidos/commit/5e831de4c87a03a7d6bf7e3726b374306b153bee))


### Bug Fixes

* update ci ([662ec4a](https://github.com/signalbreak-labs/eidos/commit/662ec4a9ad033d6eb54b304105e1fd923c6c9424))
* update ci for brew ([4a0eb17](https://github.com/signalbreak-labs/eidos/commit/4a0eb17e40f6728e30ba440bf7a49dbaacf9bdab))
* update releaser ([904520f](https://github.com/signalbreak-labs/eidos/commit/904520f806e0079e8078b3ef5d4c2007951117bd))


### Documentation

* fix inaccuracies and formatting ([ade480b](https://github.com/signalbreak-labs/eidos/commit/ade480b5f383333e98842fc92ceb93fdd88ce3ee))

## [Unreleased]
