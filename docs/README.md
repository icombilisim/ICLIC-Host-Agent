# ICLIC Host Agent — Documentation / Dokümantasyon

> **Version / Sürüm** v0.15.0 · **Last updated / Son güncelleme** 2026-06-22
> · **Canonical language / Kanonik dil** English (`en/`)

This is the documentation home for the **ICLIC Host Agent** — a small,
single-purpose Go monitoring agent that reports host + service health from a
server to the ICLIC license authority over an outbound-only HTTPS heartbeat.

Bu sayfa **ICLIC Host Agent** dokümantasyonunun giriş kapısıdır — bir sunucudan
ICLIC lisans otoritesine, yalnızca dışa giden HTTPS heartbeat'i ile host ve
servis sağlığını raporlayan küçük, tek amaçlı bir Go izleme ajanı.

> **Canonical note / Kanonik not:** English is the source of truth. Every change
> lands in `en/` first; the Turkish mirror in `tr/` follows.
> İngilizce ana kaynaktır. Her değişiklik önce `en/`'e girer; `tr/` altındaki
> Türkçe ayna onu takip eder.

---

## 📚 Documents / Dokümanlar

| Topic | 🇬🇧 English | 🇹🇷 Türkçe | What it covers / İçeriği |
|-------|-----------|-----------|--------------------------|
| **Overview** | [`en/overview.md`](en/overview.md) | [`tr/genel-bakis.md`](tr/genel-bakis.md) | What the agent is, why it exists, core invariants / Ajan nedir, neden var, temel ilkeler |
| **Architecture** | [`en/architecture.md`](en/architecture.md) | [`tr/mimari.md`](tr/mimari.md) | Module map, control channel, runtime model / Modül haritası, control kanalı, çalışma modeli |
| **Protocol** | [`en/protocol.md`](en/protocol.md) | [`tr/protokol.md`](tr/protokol.md) | Heartbeat wire contract, auth, versioning / Heartbeat sözleşmesi, kimlik doğrulama, versiyonlama |
| **Collectors** | [`en/collectors.md`](en/collectors.md) | [`tr/toplayicilar.md`](tr/toplayicilar.md) | YAML-driven collector primitives reference / YAML tabanlı toplayıcı primitive referansı |
| **Deployment** | [`en/deployment.md`](en/deployment.md) | [`tr/deploy-rehberi.md`](tr/deploy-rehberi.md) | Release, install, upgrade, rollback, fleet deploy / Release, kurulum, yükseltme, rollback, fleet deploy |

---

## 🧭 Where to start / Nereden başlamalı

- **New here? / Yeni misin?** → Overview → Architecture
- **Integrating ICLIC? / ICLIC entegrasyonu mu?** → Protocol
- **Operating a host? / Sunucu mu işletiyorsun?** → Collectors + Deployment
- **Full design rationale / Tüm tasarım gerekçesi:** parent repo `.claude/docs/` +
  ICLIC `docs/iclic-icosys-integration-surface.md`

---

> Each document carries its own version + timestamp stamp under the title. When
> you change a document, bump that stamp to match the current release.
> Her doküman başlığının altında kendi sürüm + zaman damgasını taşır. Bir
> dokümanı değiştirdiğinde damgayı güncel release'e göre güncelle.
