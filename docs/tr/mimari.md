# Mimari

> **Sürüm** v0.15.0 · **Son güncelleme** 2026-06-22 · **Kanonik dil** İngilizce
> · [ICLIC Host Agent dokümanları](../README.md) bütününün parçası

## Modül haritası

```
cmd/
└─ agent/main.go          giriş noktası: flag'ler, single-instance lock, wire-up, run loop

internal/
├─ config/
│  └─ config.go           JSON config yükleme ($ICLIC_AGENT_CONFIG, varsayılan
│                         /etc/iclic-host-agent/config.json) + env override'ları
├─ heartbeat/
│  └─ heartbeat.go        AgentVersion sabiti (release-please yönetir), 60 sn tick,
│                         POST /heartbeat + per-item /runtime-instances/heartbeat
├─ collectors/
│  ├─ loader.go           collectors.d/*.yaml okur, her tick'te binding'leri birleştirir
│  ├─ binding.go          binding şekli (id, primitive, args, output_key)
│  ├─ registry.go         primitive adı → uygulama
│  ├─ runner.go           per-tick yürütme: timeout bütçesi, hatada WARN
│  ├─ services.go         runtime.services → runtime_instances sinyalleri
│  └─ primitives_*.go     28 built-in: procfs, os, disk, exec, systemd,
│                         net (tcp/http/ssl), file, apt, docker
└─ control/
   ├─ control.go          dışa WebSocket, capability advertisement, verb dispatch
   ├─ config.go           control.yaml allow-list (yoksa = hiçbir şey sunulmaz)
   └─ metrics.go          metrics.live örnekleyici (CPU/bellek/load frame'leri)

configs/                  gelen YAML profilleri (00-linux-host … 93-vitals) + services.d/
installer/                install.sh, deploy-all.sh, systemd unit, inventory örnekleri
```

## Çalışma modeli

Ajan, systemd tarafından denetlenen (`Restart=on-failure`) tek bir Go
process'idir. Başlarken:

1. Flag'leri parse eder. `--version` `AgentVersion`'ı yazıp çıkar; bilinmeyen
   argümanlar fail-fast. Bir **single-instance lock** (flock), kopya ajanların
   aynı heartbeat'i yarıştırmasını önler — bir `--version` çağrısı asla tam bir
   ajan başlatmamalı. (#26)
2. `config.json`'ı yükler (enrolment `kid`/`secret`, `iclic_url`, interval).
3. Bir Go runtime soft bellek tavanı uygular (`debug.SetMemoryLimit`, varsayılan
   ~384 MB, `GOMEMLIMIT` ile override edilebilir).
4. **Heartbeat döngüsünü** (60 sn tick) ve `control.yaml` izin veriyorsa
   **control kanalı** goroutine'ini başlatır.

**Exponential backoff yoktur.** systemd restart politikası ve sabit 60 saniyelik
tick, kasıtlı olarak tek yeniden deneme mekanizmasıdır — heartbeat'ler idempotent
state-overwrite olduğundan, kaçan bir tick bir sonrakinde kendini iyileştirir.

## Heartbeat yolu

```
tick (60 sn)
  → loader: collectors.d/*.yaml oku → birleşik binding listesi
  → runner: her binding'i çalıştır (per-binding timeout, 30 sn toplam bütçe)
            primitive hatası → WARN + o key'i atla (asla çökme)
  → metrics{} map'ini topla
  → POST /api/v1/server/{id}/heartbeat   (Bearer <kid>.<secret>)
  → her metrics.runtime_instances[i] için:
        POST /api/v1/server/runtime-instances/heartbeat   (per-item, fatal değil)
```

Toplayıcı hattı **YAML tabanlıdır**: primitive'ler her tick'te taze okunan
binding dosyalarıyla bağlanır, yani kontrol eklemek/çıkarmak **ajan restart'ı
gerektirmez**. Primitive referansı için [`toplayicilar.md`](toplayicilar.md),
wire sözleşmesi için [`protokol.md`](protokol.md).

## Control kanalı (anlık, opt-in)

Heartbeat'in ötesinde ajan, ICLIC'e tek bir **dışa** WebSocket tutar
(`wss://<iclic>/api/v1/server/control`, aynı `<kid>.<secret>` bearer). ICLIC bu
açık soket üzerinden anlık veri ve aksiyon *ister*; ajan otoritedir ve yalnızca
kapalı, tipli bir verb kümesi sunar. **Bu bir uzak shell değildir — asla keyfi
komut çalıştırma yoktur.**

- **Yalnızca dışa.** Ajan ICLIC'e bağlanır; içeri port yok. NAT/firewall
  arkasında çalışır, heartbeat ile aynı güven yolu.
- **İstek/yanıt, kararı ajan verir.** ICLIC tipli bir `req` gönderir; ajan yerel
  doğrular ve `res` frame'lerini geri akıtır. Bilinmeyen veya izinsiz verb'ler
  çalıştırılmaz, reddedilir.
- **Opt-in, varsayılan KAPALI.** Yalnızca `/etc/iclic-host-agent/control.yaml`
  (operatöre ait) içinde açıkça etkinleştirilen verb'ler sunulur. Bu dosya yoksa
  ajan bağlanır ama her isteği reddeder.
- **Capability advertisement.** Bağlanırken ajan OS'unu ve izin verdiği tam
  verb/hedef kümesini bildirir; Fleet UI yalnızca her host'un izin verdiğini
  gösterir.
- **Yıkıcı aksiyonlar korumalı.** Yönetim verb'leri (restart, deploy, prune) ayrı
  ayrı opt-in ister; yıkıcı olanlar ek olarak ICLIC tarafında operatör 2FA
  step-up ister ve her istek audit'lenir.

### Gelen verb'ler (read)

`logs.tail` (canlı/follow) · `proc.top` · `proc.top.live` (otomatik tazelenen
top) · `disk.df` · `net.listen` · `cron.list` (crontab + cron.d + systemd timer)
· `svc.status` (çalışan + hatalı servisler) · `svc.list` (tam servis envanteri) ·
`pkg.list` (kurulu OS paketleri, dpkg/rpm) · `docker.ps` · `metrics.live`
(CPU/bellek/load örnekleri). `svc.list` ve `pkg.list` anlık sunucu raporunu besler
(ICLIC #766). Write/yönetim verb'leri sıradaki faz (ICLIC #339).

### Opt-in yapılandırması

```yaml
# /etc/iclic-host-agent/control.yaml   (yoksa = kanal hiçbir şey sunmaz)
control:
  enabled: true
  logs:
    enabled: true
    default_lines: 200
    max_lines: 2000            # ajan bunu kapar; ICLIC aşamaz
    max_follow_seconds: 600    # canlı tail'ler bundan sonra otomatik durur
    sources:                   # mantıksal ad -> somut kaynak (host'a göre)
      icglb: { type: docker,   container: icosys-icglb }
      nginx: { type: file,     path: /var/log/nginx/error.log }
  top:   { enabled: true }     # proc.top + proc.top.live
  df:    { enabled: true }     # disk.df
  ports: { enabled: true }     # net.listen
  cron:  { enabled: true }     # cron.list
  svc:   { enabled: true }     # svc.status + svc.list
  pkg:   { enabled: true }     # pkg.list (kurulu OS paketleri)
  docker: { enabled: true }    # docker.ps
  # actions: (write verb'leri — restart/deploy/prune) ICLIC #339 ile gelir
```

Varsayılanlar (`default_lines=200`, `max_lines=2000`, `max_follow_seconds=600`)
ajan tarafında zorlanır — ICLIC, host'un bildirdiği tavanı aşamaz.

## Dosya sistemi düzeni

```
/opt/iclic-host-agent/
├─ bin/
│  ├─ iclic-host-agent-v0.14.0     # versiyonlu binary
│  └─ iclic-host-agent-v0.15.0     # versiyonlu binary (yükseltmeden sonra)
└─ iclic-host-agent               # symlink → bin/iclic-host-agent-v0.15.0

/etc/iclic-host-agent/
├─ config.json                    # 0640 root:iclic-agent — enrolment creds
├─ control.yaml                   # opsiyonel — control-kanalı allow-list
└─ collectors.d/
   ├─ 00-linux-host.yaml
   └─ … (hangi profiller aktifse)

/var/lib/iclic-host-agent/
└─ state.json                     # 0600 iclic-agent:iclic-agent

/etc/systemd/system/iclic-host-agent.service
/etc/systemd/system/iclic-host-agent.service.d/   # operator drop-in (memory, env, pprof)
```

Ajan `iclic-agent` sistem kullanıcısı olarak çalışır. Versiyonlu binary'ler +
`current` symlink, rollback'i tek `ln -sfn` uzağında tutar.

## Bellek ve teşhis

v0.3.x uzun uptime'larda bellek sızdırıyordu (paylaşılan bir `http.Transport`
eksikti). v0.4.0+ bunu düzeltti ve savunma katmanladı:

- **Go soft cap:** `debug.SetMemoryLimit` (~384 MB varsayılan), `GOMEMLIMIT`
  override.
- **systemd cgroup hard cap:** operatörler bir `memory.conf` drop-in ekler
  (`MemoryHigh=384M`, `MemoryMax=512M`); kaçak bir process OOM-kill edilir ve
  restart edilir.
- **pprof yalnızca loopback:** `127.0.0.1:6133/debug/pprof/*`, yalnızca SSH
  port-forward ile erişilir; `ICLIC_AGENT_PPROF_ADDR=disabled` ile kapatılır.

Operatör komutları için [`deploy-rehberi.md`](deploy-rehberi.md) §"Bellek
kontrolü ve teşhis".
