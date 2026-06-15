# Changelog

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
