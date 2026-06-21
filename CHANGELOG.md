# Changelog

## [0.15.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.14.0...v0.15.0) (2026-06-21)


### Features

* **heartbeat:** apply server-driven interval at runtime via ticker reset ([#31](https://github.com/icombilisim/ICLIC-Host-Agent/issues/31)) ([f5f5a46](https://github.com/icombilisim/ICLIC-Host-Agent/commit/f5f5a4660a56e423cfab081fbb9e3b7fa9528e10))

## [0.14.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.13.1...v0.14.0) (2026-06-19)


### Features

* **metrics:** report swap usage via procfs.swap ([#28](https://github.com/icombilisim/ICLIC-Host-Agent/issues/28)) ([#29](https://github.com/icombilisim/ICLIC-Host-Agent/issues/29)) ([83c6f73](https://github.com/icombilisim/ICLIC-Host-Agent/commit/83c6f734ce34f7c301814e6e9fb621402b7523ad))

## [0.13.1](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.13.0...v0.13.1) (2026-06-17)


### Bug Fixes

* add --version flag, fail-fast on unknown args, single-instance lock ([#26](https://github.com/icombilisim/ICLIC-Host-Agent/issues/26)) ([b974281](https://github.com/icombilisim/ICLIC-Host-Agent/commit/b9742811b30f63db817b75526b97e28628e4d4ab)), closes [#25](https://github.com/icombilisim/ICLIC-Host-Agent/issues/25)

## [0.13.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.12.0...v0.13.0) (2026-06-17)


### Features

* **metrics:** add cpu_used_pct to the heartbeat for metric history ([#388](https://github.com/icombilisim/ICLIC-Host-Agent/issues/388)) ([#23](https://github.com/icombilisim/ICLIC-Host-Agent/issues/23)) ([6bdb9fe](https://github.com/icombilisim/ICLIC-Host-Agent/commit/6bdb9fea00c1abb7123f3c969ad1927f91d8625e))

## [0.12.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.11.0...v0.12.0) (2026-06-17)


### Features

* **control:** add metrics.live verb streaming CPU/mem/load samples ([#379](https://github.com/icombilisim/ICLIC-Host-Agent/issues/379)) ([#21](https://github.com/icombilisim/ICLIC-Host-Agent/issues/21)) ([5478369](https://github.com/icombilisim/ICLIC-Host-Agent/commit/5478369f13508dec7d7e8552dc20c7d87a0577a4))

## [0.11.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.10.0...v0.11.0) (2026-06-17)


### Features

* **control:** add svc.status and docker.ps read verbs ([#375](https://github.com/icombilisim/ICLIC-Host-Agent/issues/375)) ([0f1ac75](https://github.com/icombilisim/ICLIC-Host-Agent/commit/0f1ac75da588ff33c1596d819f8a39dc5d12e71e))
* **control:** add svc.status and docker.ps read verbs ([#375](https://github.com/icombilisim/ICLIC-Host-Agent/issues/375)) ([d3f5a93](https://github.com/icombilisim/ICLIC-Host-Agent/commit/d3f5a93150eb4acd26b44e8a6a21da21ff93ce7b))

## [0.10.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.9.0...v0.10.0) (2026-06-15)


### Features

* **collectors:** include published container ports in docker.containers ([#348](https://github.com/icombilisim/ICLIC-Host-Agent/issues/348)) ([494dff9](https://github.com/icombilisim/ICLIC-Host-Agent/commit/494dff95918463a6fc772b7480707981dc781312))
* **collectors:** include published container ports in docker.containers ([#348](https://github.com/icombilisim/ICLIC-Host-Agent/issues/348)) ([8315c52](https://github.com/icombilisim/ICLIC-Host-Agent/commit/8315c523059e849de815cec431abc512d3fbef15))

## [0.9.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.8.0...v0.9.0) (2026-06-15)


### Features

* **control:** add proc.top.live + cron.list read verbs ([#348](https://github.com/icombilisim/ICLIC-Host-Agent/issues/348)) ([cbfce78](https://github.com/icombilisim/ICLIC-Host-Agent/commit/cbfce7860ac185a2872cbe70047d3c89873b5320))
* **control:** add proc.top.live + cron.list read verbs ([#348](https://github.com/icombilisim/ICLIC-Host-Agent/issues/348)) ([258ee36](https://github.com/icombilisim/ICLIC-Host-Agent/commit/258ee366128363501c03cd45ec5c2a64a0cff232))

## [0.8.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.7.0...v0.8.0) (2026-06-15)


### Features

* **agent:** advertise services in heartbeat ([#342](https://github.com/icombilisim/ICLIC-Host-Agent/issues/342), Faz 4d-3a) ([5bfde6e](https://github.com/icombilisim/ICLIC-Host-Agent/commit/5bfde6e1695183beaa0d40727081f26669e9eeea))

## [0.7.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.6.0...v0.7.0) (2026-06-15)


### Features

* **agent:** service logs → control channel ([#342](https://github.com/icombilisim/ICLIC-Host-Agent/issues/342), Faz 4d-2) ([c77d277](https://github.com/icombilisim/ICLIC-Host-Agent/commit/c77d277ed82e2eb041861779de1316a3ad8c4b28))

## [0.6.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.5.0...v0.6.0) (2026-06-15)


### Features

* **agent:** service definitions — axis expander + heartbeat wiring ([#342](https://github.com/icombilisim/ICLIC-Host-Agent/issues/342), Faz 4d-1) ([f25f653](https://github.com/icombilisim/ICLIC-Host-Agent/commit/f25f6535f9222576611a85c2231cb3d11ff4e26f))
* **agent:** service definitions — composable axis expander + heartbeat wiring ([#342](https://github.com/icombilisim/ICLIC-Host-Agent/issues/342)) ([64bd406](https://github.com/icombilisim/ICLIC-Host-Agent/commit/64bd406c7fcfa5d3807b9248b1a0c898102f8b95))

## [0.5.0](https://github.com/icombilisim/ICLIC-Host-Agent/compare/v0.4.2...v0.5.0) (2026-06-15)


### Features

* add outbound control-channel client ([#337](https://github.com/icombilisim/ICLIC-Host-Agent/issues/337)) ([21981ed](https://github.com/icombilisim/ICLIC-Host-Agent/commit/21981edb561a861ac463b159a594ae06891399ec))
* **agent:** add ssl.cert_expiry primitive for TLS cert-expiry monitoring ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([0d27d18](https://github.com/icombilisim/ICLIC-Host-Agent/commit/0d27d18d4eae4b579483f7ff3a8936002d056698))
* **agent:** backup-freshness metrics — file.newest_age_seconds + file.stat age_seconds ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([a98a73c](https://github.com/icombilisim/ICLIC-Host-Agent/commit/a98a73c0c1eee809b11b7a194ea98ad83c8d9271))
* **agent:** disk I/O + network vitals — procfs.diskstats + procfs.netdev ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([57e3bfa](https://github.com/icombilisim/ICLIC-Host-Agent/commit/57e3bfa05982f78cce9437a131be33e17a3cf745))
* **agent:** http.probe primitive for synthetic uptime/latency ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([e0e3e64](https://github.com/icombilisim/ICLIC-Host-Agent/commit/e0e3e643f4c714794acace3774f67c192aadc9f6))
* **agent:** W1 outage-preventers — cert expiry, backup freshness, uptime/latency ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([475c377](https://github.com/icombilisim/ICLIC-Host-Agent/commit/475c377f477fda8b5b43dda6cc40470f79fb472d))
* **agent:** W1 vitals — disk I/O + network throughput ([#40](https://github.com/icombilisim/ICLIC-Host-Agent/issues/40)) ([c4eeed3](https://github.com/icombilisim/ICLIC-Host-Agent/commit/c4eeed33d7c3e69e5c7360a3e40e4b2e9a1f1131))
* control-channel read verbs (logs.tail, proc.top, disk.df, net.listen) ([#337](https://github.com/icombilisim/ICLIC-Host-Agent/issues/337)) ([801b4e5](https://github.com/icombilisim/ICLIC-Host-Agent/commit/801b4e5ce478d25f7415a42cc9af1696a8987f3e))
* outbound control channel + read verbs (logs.tail, proc.top, disk.df, net.listen) ([#337](https://github.com/icombilisim/ICLIC-Host-Agent/issues/337)) ([bb2d911](https://github.com/icombilisim/ICLIC-Host-Agent/commit/bb2d911cebf866cded9f4c2388c8b7b6e733cf39))
