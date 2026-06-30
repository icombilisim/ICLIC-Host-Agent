# ICLIC Host Agent — Genel Bakış

> **Sürüm** v0.15.0 · **Son güncelleme** 2026-06-22 · **Kanonik dil** İngilizce
> · [ICLIC Host Agent dokümanları](../README.md) bütününün parçası

> Hem insanlar **hem de AI kodlama araçları** için referans doküman. Önce bunu,
> sonra [`mimari.md`](mimari.md), [`protokol.md`](protokol.md),
> [`toplayicilar.md`](toplayicilar.md), [`deploy-rehberi.md`](deploy-rehberi.md)
> okuyun.

## Nedir

Bir sunucudan **ICLIC lisans otoritesine** host ve servis sağlığını raporlayan,
küçük ve tek amaçlı bir **izleme ajanı**. **Sıkıcı ve denetlenebilir** olacak
şekilde tasarlandı — ONPREM müşteriler kurmadan önce her satırını okuyabilmeli.

Ajan her 60 saniyede bir `/etc/iclic-host-agent/collectors.d/*.yaml` içinde
tanımlı binding'leri çalıştırır, sonuçları tek bir map'e paketler ve enrolment
sırasında verilen PAT-tarzı bearer anahtarıyla (`Bearer <kid>.<secret>`) HTTPS
üzerinden ICLIC'e POST eder. Bu heartbeat **yalnızca push'tur**.

## Neden var

- **Tek ekrandan fleet sağlığı:** her yönetilen host için CPU/RAM/disk, container
  durumu, systemd unit'leri ve ICOSYS/ICLIC servis versiyonları tek bir ICLIC
  görünümüne düşer.
- **İçeri açık port yok:** izlenen host hiçbir port açmaz. Ajan dışarı bağlanır;
  ICLIC içeri hiç bağlanmaz. NAT/firewall arkasında çalışır.
- **Kodla değil operatörle yönlendirilir:** neyin ölçüleceği binary'ye derlenmez,
  YAML binding'lerinde tanımlanır. Yeni kontrol = bir YAML dosyası bırak.
- **Müşteri tarafından denetlenebilir:** gizli davranış yok, shell geçişi yok,
  uygulama verisi veya `/home` okuması yok.

## Ne YAPMAZ

- YAML binding'lerinde tanımlananın dışında shell çalıştırmaz.
- Kendi state dosyası (`/var/lib/iclic-host-agent/state.json`) dışında dosyaya
  yazmaz.
- Yapılandırılmış ICLIC URL'i dışında dışa trafik üretmez.
- `/etc/passwd`, `/home` veya uygulama verisini okumaz.
- İçeri açık port yok ve **shell geçişi yok** — control kanalı, ajanın **dışa
  doğru bağladığı**, yalnızca opt-in ve kapalı bir tipli komut (verb) kümesini
  sunan bir sokettir (varsayılan: KAPALI). ICLIC ister; ajan karar verir.

## İki kanal

| Kanal | Yön | Sıklık | Amaç |
|-------|-----|--------|------|
| **Heartbeat** | ajan → ICLIC (HTTPS POST) | her 60 sn | Host + servis metriklerini push'lar |
| **Control** | ajan → ICLIC (dışa WebSocket) | açık tutulur, opt-in | ICLIC, kapalı bir verb kümesi üzerinden anlık veri *ister* |

İkisi de aynı `<kid>.<secret>` bearer'ı kullanır. Hiçbiri içeri port açmaz.
Control kanalı detayı için [`mimari.md`](mimari.md).

## Toplayıcı profilleri (gelenler)

Ajanın metrik gövdesi operatörün seçtiği YAML profillerinden oluşur. Her biri
bağımsız kurulabilir — yalnızca nginx ve Postgres çalıştıran bir host
`host,nginx,devops` kullanır, fazlası değil.

| Profil | Dosya | Kapsam |
|--------|-------|--------|
| `host` | `00-linux-host.yaml` | CPU load, bellek, disk, uptime, OS, kernel, güvenlik güncelleme sayısı |
| `docker` | `10-docker.yaml` | Container özeti + per-container stats + yayınlanan portlar (`/var/run/docker.sock`) |
| `systemd` | `20-systemd.yaml` | Belirtilen systemd unit'lerinin kaynak kullanımı (cgroup) |
| `icosys` | `30-icosys-actuator.yaml` | ICOSYS Spring Boot servisleri (icglb 8010 … icwfl 8060) — `runtime_instances`, health, versiyon, git commit |
| `mysql` | `40-mysql.yaml` | MySQL canlılığı + versiyon |
| `redis` | `50-redis.yaml` | Redis canlılığı + ping + versiyon |
| `nginx` | `60-nginx.yaml` | nginx canlılığı + versiyon + 80/443 portları |
| `iclic` | `70-iclic.yaml` | ICLIC Spring Boot actuator (port 8001) |
| `devops` | `80-devops-stack.yaml` | Nexus + SonarQube + Dokploy + Postgres |

Ek olarak TLS son kullanma (`90-tls.yaml`), yedekler (`91-backups.yaml`), uptime
(`92-uptime.yaml`) ve host vitals (`93-vitals.yaml`) profilleri gelir.
[`toplayicilar.md`](toplayicilar.md)'ye bakın.

## Temel ilkeler (bozma)

1. **Yalnızca dışa.** Ajan ICLIC'e bağlanır; asla içeri port açmaz.
2. **Keyfi komut çalıştırma yok.** Control kanalı yalnızca opt-in, kapalı, tipli
   bir verb kümesi sunar — asla bir shell değil.
3. **Varsayılan opt-in (kapalı).** `control.yaml` yoksa ajan bağlanır ama her
   control isteğini reddeder. Yıkıcı verb'ler ayrıca ICLIC tarafında 2FA ister.
4. **Tek ICLIC URL'i.** Bir ajan → bir ICLIC. Başka dışa hedef yok.
5. **Versiyonlu binary'ler.** Rollback, `current` symlink'in tek bir `ln -sfn`'i —
   önceki binary diskte kalır.
6. **Versiyonu release-please yönetir.** `AgentVersion`'ı elle bump etme, `v*`
   tag'i elle push etme.

## Durum (v0.15.0)

Heartbeat omurgası fleet genelinde canlı: 28 YAML tabanlı primitive, dokuz
profil, `runtime_instances` deployment sinyalleri ve PII içermeyen, push-only bir
heartbeat. Control kanalı **read verb'leri** geldi — `logs.tail`, `proc.top`,
`proc.top.live`, `disk.df`, `net.listen`, `cron.list`, `svc.status`, `svc.list`,
`pkg.list`, `docker.ps` ve `metrics.live`
(CPU/bellek/load akışı). **Write/yönetim verb'leri** (restart/deploy/prune, ICLIC
tarafında 2FA korumalı) sıradaki faz.

Takip: ICLIC #40 · #337 (read) · #348 (live top + cron) · #339 (write).
