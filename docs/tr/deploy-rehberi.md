# ICLIC Host Agent — Deploy Rehberi (Aptala Anlatır Gibi)

> **Sürüm** v0.15.0 · **Son güncelleme** 2026-06-24 · **Kanonik dil** İngilizce
> · [ICLIC Host Agent dokümanları](../README.md) bütününün parçası

Bu doküman ICLIC Host Agent'ı release etmek, sunuculara kurmak, yükseltmek ve
rollback yapmak için adım adım bir kılavuzdur. Hiçbir şey ezbere bilinmesi
gerekmiyor — her adımda ne olacağını yazdım.

---

## 1. Sistem Ne Yapıyor?

Her sunucuda küçük bir Go binary çalışıyor (`iclic-host-agent`). Bu binary:

- Her **60 saniyede bir** uyanıyor.
- `/etc/iclic-host-agent/collectors.d/*.yaml` dosyalarını okuyup ne ölçeceğini
  öğreniyor (CPU, RAM, MySQL portu açık mı, ICOSYS servisi sağlıklı mı, vs.).
- Sonuçları **ICLIC sunucusuna POST ediyor** (`/api/v1/server/{id}/heartbeat`).
- O kadar. Hiçbir port açmıyor, hiçbir komut almıyor — sadece push.

ICLIC tarafında bu sinyalleri görmek için: **Servers** ekranı → ilgili sunucuya
tıkla → host metrikleri ve agent versiyonu gözükür.

---

## 2. Mimari — Tek Bakışta

```
   ┌─────────────────────────┐
   │  ICOSYS / DevOps / ICLIC│
   │       sunucusu          │
   │  iclic-host-agent       │
   │  (systemd unit, 60s tick)│
   │     reads /proc, os-release,
   │     docker.sock, http,  │
   │     collectors.d/*.yaml │
   │           │             │
   │           ▼  HTTPS      │
   │  POST /api/v1/server/{id}/heartbeat
   │  Authorization: Bearer <kid>.<secret>
   └─────────────┬───────────┘
                 │
                 ▼
   ┌─────────────────────────┐
   │  ICLIC  (Spring Boot 8001)│
   │  heartbeat controller → │
   │  Server timeline → detay│
   └─────────────────────────┘
```

**Önemli:** Agent ICLIC'e gidiyor, ICLIC agent'a hiç gitmiyor. Yani sunucudan
internete sadece **dışa giden** HTTPS yeterli. İçeri açık port gerekmez.

---

## 3. Profiller — Ne Hangi Sunucuya?

Agent, "şu kadar şeyi ölç" listesini YAML dosyalarından alıyor. Her YAML bir
**profil**. Operator (sen) hangi profilleri kuracağına karar veriyorsun. Primitive
referansı için [`toplayicilar.md`](toplayicilar.md).

| Profil | YAML Dosyası | Ne Ölçer |
|---|---|---|
| `host` | `00-linux-host.yaml` | CPU, RAM, disk, uptime, OS, kernel — **HER SUNUCUDA OLMALI** |
| `docker` | `10-docker.yaml` | Docker container sayısı + per-container stats |
| `security` | `15-security.yaml` | Güvenlik servis matrisi + health ve log-tazelik envanteri (operatöre göre düzenlenir; unit/log listesini sunucunun stack'ine/SIEM'ine göre değiştirin) |
| `systemd` | `20-systemd.yaml` | systemd unit'lerin cgroup CPU/MEM kullanımı |
| `icosys` | `30-icosys-actuator.yaml` | 6 ICOSYS Spring Boot servisi (8010-8060) — health/heap/threads |
| `mysql` | `40-mysql.yaml` | MySQL portu açık mı + versiyon |
| `redis` | `50-redis.yaml` | Redis portu + ping + versiyon |
| `nginx` | `60-nginx.yaml` | nginx servisi + 80/443 portları + versiyon |
| `iclic` | `70-iclic.yaml` | ICLIC Spring Boot (port 8001) actuator |
| `devops` | `80-devops-stack.yaml` | Nexus + SonarQube + Dokploy + Postgres |
| `aigw-test` | `90-aigw-test.yaml` | TEST sunucusundaki AI Gateway (`icosys-aigw`, port 8095) |
| `aigw-prod` | `90-aigw-prod.yaml` | ICLIC-PROD sunucusundaki AI Gateway (`iclic-aigw`, port 8095) |

### Hangi sunucuya hangileri?

| Sunucu | Profiller |
|---|---|
| ICOSYS test | `host,docker,systemd,icosys,mysql,redis,nginx,aigw-test` |
| ICOSYS prod | `host,docker,systemd,icosys,mysql,redis,nginx` |
| DevOps | `host,docker,systemd,devops` |
| ICLIC prod | `host,docker,systemd,iclic,aigw-prod` |

**Kural:** Sunucuda olan şeyleri profil olarak ekle, olmayanları ekleme.

> **AI Gateway neden iki profile bölündü?** ICLIC, runtime instance'ları
> `(runtime_component, instance_key)` üzerinden tekilleştiriyor ve raporlayan
> sunucuyu bu anahtara katmıyor; agent ise `instance_key`'i olduğu gibi
> gönderiyor. Bu yüzden test ve prod gateway'lerinin *farklı* `instance_key`
> değerleri olmalı — `aigw-test` ve `aigw-prod` her sunucu için doğru container
> adını ve anahtarı taşıyor. Her gateway sunucusuna sadece birini koy, ikisini
> birden asla.
DevOps'ta MySQL yoksa `mysql` profili eklemeye gerek yok — agent o port'u boşuna
probe etmesin.

---

## 4. Release Akışı (Maintainer'ın İşi) — release-please

Versiyonu **release-please** yönetiyor. **`AgentVersion`'ı elle bump etme, `v*`
tag'i elle atma** — ikisinin de sahibi release-please (manifest
`.release-please-manifest.json`, config `release-please-config.json`).

```
1. main'e Conventional-Commit PR'ları gir  (feat: → minor, fix: → patch)
2. release-please bir release PR açar/günceller:
      chore(main): release X.Y.Z
   → AgentVersion'ı bump eder (internal/heartbeat/heartbeat.go içindeki
     x-release-please-version annotation'ı) + CHANGELOG
3. Release PR'ı merge et
   → release-please vX.Y.Z tag atar + GitHub Release oluşturur
   → build job şunları ekler:
        iclic-host-agent-linux-amd64
        iclic-host-agent-linux-arm64
        configs.tar.gz   (tüm YAML profilleri)
        install.sh
        iclic-host-agent.service
        SHA256SUMS
4. Yayına al: smoke-test sonrası prod inventory'ye karşı deploy-all.sh
```

Tag (`vX.Y.Z`) ile koddaki sabit uyuşmazsa build fail eder — bu kasıtlı.
Heartbeat'te yanlış versiyon raporlanmasın diye.

---

## 5. İlk Kurulum Akışı (Yeni Sunucuya)

Bir sunucuya **ilk kez** agent kurarken: önce **ICLIC UI → Servers → "New
Server"** ile sunucuyu kaydet, ICLIC sana **tek kullanımlık, TTL'li bir TOKEN**
versin. Sonra sunucuya SSH yap ve:

```bash
curl -fsSL https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/install.sh \
  -o /tmp/install.sh

sudo TOKEN=<verilen-token> \
     ICLIC_URL=https://iclic.app \
     PROFILES=host,docker,systemd,icosys,mysql,redis,nginx \
     bash /tmp/install.sh
```

`install.sh` ne yapıyor:

```
a. iclic-agent kullanıcısı oluştur
b. /opt/iclic-host-agent dizinleri kur
c. Latest release'i indir + SHA256 doğrula
d. Binary'yi bin/iclic-host-agent-vX.Y.Z olarak yaz
e. Symlink: iclic-host-agent → bin/...vX.Y.Z
f. TOKEN'ı /api/v1/agent/enroll'a POST et → kalıcı bearer al
g. /etc/iclic-host-agent/config.json yaz
h. PROFILES'taki YAML'ları collectors.d/ içine kopyala
i. systemd unit'i kur ve başlat
```

**Token notu:** Token tek kullanımlık. install.sh enroll adımında hata alırsa,
TOKEN yenisi olmadan tekrar denersen ICLIC reddeder. Bu durumda ICLIC'ten yeni
bir token üret ve baştan başla.

Doğrulama: `systemctl status iclic-host-agent` + `journalctl -u
iclic-host-agent -f`. 60 sn içinde ICLIC dashboard'unda `enrollment_status`,
`PENDING_ENROLLMENT` → `HEALTHY`'ye döner.

---

## 6. Upgrade Akışı (Tek Sunucu — Re-run install.sh)

Sunucu zaten enroll olmuş, sadece yeni versiyona geçiyoruz (TOKEN gerekmiyor —
`config.json` zaten var):

```bash
# Son release, mevcut profiller
sudo bash /tmp/install.sh

# Spesifik tag'e sabitle
sudo AGENT_VERSION=v0.15.0 bash /tmp/install.sh

# Profil ekle / değiştir
sudo PROFILES=host,docker,systemd,icosys,mysql,redis bash /tmp/install.sh
```

`config.json` korunur. Yeni binary `bin/iclic-host-agent-<tag>` olarak iner,
symlink retarget olur, systemd unit'i restart eder. Önceki binary diskte kalır —
rollback gerektiğinde lazım olur.

---

## 7. Fleet Deploy (Tüm Sunucular Birden — `deploy-all.sh`)

`deploy-all.sh` SSH ile sırayla her sunucuya bağlanıp install.sh'i çalıştırır.

```bash
cd installer
cp inventory.example inventory.local
$EDITOR inventory.local          # satır başına bir host: host:profiles[:user[:port]]
bash deploy-all.sh inventory.local v0.15.0
```

`inventory.local` örneği (gerçek IP'ler, gerçek user'lar):

```
<icosys-test-ip>:host,docker,systemd,icosys,mysql,redis,nginx:icadmin
<icosys-prod-ip>:host,docker,systemd,icosys,mysql,redis,nginx:icadmin
<devops-ip>:host,docker,systemd,devops:icadmin
```

Bir sunucu fail ederse loop devam eder; sonunda succeeded vs. failed özeti
basılır ve exit code = fail sayısı.

**Önkoşul:** SSH erişimin password-less olmalı (key-based auth) ve hedef
sunucularda `sudo -n bash install.sh` çalışmalı (sudoers'ta NOPASSWD). Aksi halde
script ortada takılır.

**Önemli:** `deploy-all.sh` SADECE upgrade içindir. İlk kurulum (TOKEN gerektiren)
her sunucuda elle yapılır — token sunucu başına farklı çünkü.

---

## 8. Rollback Akışı

Yeni versiyon bozuk çıkarsa, eski binary diskte zaten duruyor — sadece symlink'i
geri al:

```bash
ssh icadmin@<sunucu>

ls /opt/iclic-host-agent/bin/
#   iclic-host-agent-v0.14.0
#   iclic-host-agent-v0.15.0   (şu an bozuk)

sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.14.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent
journalctl -u iclic-host-agent -n 50
```

Bu işlem **5 saniye** sürer ve config'e dokunmaz. Tüm fleet'te rollback gerekirse
`deploy-all.sh inventory.local v0.14.0` çalıştır — install.sh idempotent olduğu
için eski versiyona "yükseltir" (yani geri döner).

---

## 9. Doğrulama — Kuruldu mu, Çalışıyor mu?

### Sunucu tarafında

```bash
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f          # 60s'lik tick'leri gör
ls /etc/iclic-host-agent/collectors.d/     # hangi YAML'lar aktif
ls -la /opt/iclic-host-agent/iclic-host-agent   # symlink hangi versiyona bakıyor
```

### ICLIC tarafında

1. **Servers** → sunucuyu bul
2. `last_seen_at` 60 saniyeden taze olmalı
3. `agent_version` yüklediğin tag olmalı
4. **Server Detail** → "Host Metrics" → CPU/RAM/disk gerçek olmalı
5. "Heartbeat History" panel'de raw payload'ı gör — profilinin key'leri
   (`mysql_running`, `nginx_version`, vs.) orada gözükmeli

---

## 10. Sorun Giderme

| Belirti | Bak |
|---|---|
| install.sh fail oldu, sunucu hala enroll değil | Token expired mı? Yeni üret. `curl https://iclic.app/actuator/health` cevap veriyor mu? DNS çözülüyor mu? |
| systemctl active ama ICLIC heartbeat görmüyor | `journalctl -u iclic-host-agent -n 100`; `cat config.json` — `iclic_url` doğru mu? `PENDING_ENROLLMENT`'ta takılıysa config.json'ı sil + yeni TOKEN ile install.sh'i tekrar çalıştır. |
| Bir collector key gözükmüyor | O profil `collectors.d/`'ye yüklendi mi? Probe bu sunucuda çalıştırılabilir mi (`nginx -v`, `redis-server --version` PATH'te mi)? `journalctl -u iclic-host-agent | grep WARN`. |
| deploy-all.sh ortada takıldı | `ssh -o BatchMode=yes <host>` prompt görmeden bağlanmalı; `sudo -n true` hatasız çalışmalı (NOPASSWD). |
| SHA256 mismatch | İndirme yarıda kaldı (veya çok düşük ihtimal release tampered) — install.sh'i tekrar çalıştır. |

---

## 11. Hızlı Komut Sözlüğü

```bash
# Tek sunucuya ilk kurulum
sudo TOKEN=xyz ICLIC_URL=https://iclic.app \
     PROFILES=host,docker,systemd,icosys bash install.sh

# Tek sunucuda upgrade
sudo bash install.sh

# Spesifik versiyona git (downgrade dahil)
sudo AGENT_VERSION=v0.14.0 bash install.sh

# Fleet upgrade
bash deploy-all.sh inventory.local v0.15.0

# Tek sunucuda rollback
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.14.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent

# Verify
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f
```

---

## 12. Dosya Konumları (Cheat Sheet)

| Yer | İçerik |
|---|---|
| `/opt/iclic-host-agent/iclic-host-agent` | Symlink → aktif binary |
| `/opt/iclic-host-agent/bin/iclic-host-agent-vX.Y.Z` | Versiyonlu binary'ler |
| `/etc/iclic-host-agent/config.json` | Enrolment kimlik bilgileri (root:iclic-agent 0640) |
| `/etc/iclic-host-agent/control.yaml` | Control-kanalı allow-list (opsiyonel) |
| `/etc/iclic-host-agent/collectors.d/` | Aktif YAML profilleri |
| `/var/lib/iclic-host-agent/state.json` | Agent state (heartbeat sequence vs) |
| `/etc/systemd/system/iclic-host-agent.service` | systemd unit |
| `…/iclic-host-agent.service.d/*.conf` | Operator drop-in (memory, env, pprof) |

---

## 13. Bellek Kontrolü ve Teşhis (v0.4.0+)

v0.3.x agent uzun uptime'larda yavaşça bellek sızdırıyordu. v0.4.0 paylaşılan
`http.Transport`'a geçti + `GOMEMLIMIT` default'u + loopback pprof ekledi. Yine de
defansif iki katman öneriyoruz:

### a) Go runtime soft cap

Agent açılırken `debug.SetMemoryLimit(~384 MB)` çağırıyor. Override:

```bash
# /etc/systemd/system/iclic-host-agent.service.d/env.conf
[Service]
Environment="GOMEMLIMIT=512MiB"
```

### b) systemd cgroup hard cap

Tüm host'larda öneririz:

```bash
sudo mkdir -p /etc/systemd/system/iclic-host-agent.service.d
sudo tee /etc/systemd/system/iclic-host-agent.service.d/memory.conf >/dev/null <<'EOF'
[Service]
MemoryHigh=384M
MemoryMax=512M
EOF
sudo systemctl daemon-reload && sudo systemctl restart iclic-host-agent
```

### c) pprof (sadece localhost'tan)

Agent `127.0.0.1:6133/debug/pprof/*` üzerinde profilleri sunuyor. Dışarıya açık
değil; SSH port-forward gerek:

```bash
ssh -L 6133:127.0.0.1:6133 icadmin@<sunucu>
go tool pprof -http=:0 http://localhost:6133/debug/pprof/heap
```

Kapatmak için `ICLIC_AGENT_PPROF_ADDR=disabled` (env drop-in), adres değiştirmek
için aynı env var'a `127.0.0.1:6200` gibi bir değer ver.

---

## 14. Release imzalama (Ed25519) — oto-güncelleme temeli

Release'ler **Ed25519 ile imzalanır** ki bir host, bir release'in sadece *bozulmamış*
değil *otantik* (bizden geldiğini) olduğunu kanıtlayabilsin. `SHA256SUMS` bütünlük
verir; imza `SHA256SUMS.sig` otantiklik verir. Bu, ICLIC-orkestralı gecelik
oto-güncellemenin ön koşuludur (root olarak çekip kendini kuran bir host,
kriptografik olarak doğrulayamadığı hiçbir şeyi kabul etmemeli). (#35)

**Nasıl çalışır**
- CI (`release.yml`), `SHA256SUMS`'ı `AGENT_RELEASE_SIGNING_KEY` repo secret'ındaki
  özel anahtarla imzalar ve `SHA256SUMS.sig` yayınlar. Secret yoksa build
  **fail-closed** olur — imzasız release çıkmaz.
- `install.sh`, herhangi bir checksum'a güvenmeden **önce** imzayı doğrular:
  - **Upgrade** yolu, zaten güvenilen mevcut binary'yi kullanır
    (`iclic-host-agent verify-release --sums … --sig …`) — bağımlılıksız.
  - **Fresh install**, `openssl` + gömülü public key'e düşer; kullanılabilir
    doğrulayıcı yoksa HTTPS üzerinden TOFU ile devam eder (insan başlatır).
  - Gerçek bir imza **uyuşmazlığı her zaman durdurur**. Doğrulama hiç yapılamadığında
    da durdurmak için `STRICT_VERIFY=1` ver (oto-updater strict çalışır).

**Tek seferlik kurulum (sahip)** — ilk imzalı release'ten önce bir kez:
```bash
bash scripts/gen-release-signing-key.sh
```
Üç çıktı ve her birinin yeri:
1. **Private key PEM** → GitHub repo secret `AGENT_RELEASE_SIGNING_KEY`.
2. **Public key PEM** → `installer/install.sh` (`RELEASE_PUBKEY_PEM`).
3. **Public key base64** → `internal/release/verify.go` (`releasePublicKeyB64`).

(2) ve (3)'ü commit et, (1)'i secret olarak kaydet. O zamana kadar release'ler
imzasız kalır ve `install.sh` TOFU modunda çalışır — anahtar gömülüp release bir
`.sig` taşıdığında doğrulama otomatik devreye girer.

> Yol haritası: imzalama **Faz 1**. Faz 2–4: heartbeat `desiredAgentVersion`
> sinyali, root `iclic-host-agent-updater.timer` (gecelik 01:00 UTC, health-gate +
> auto-rollback) ve ICLIC tarafı halka orkestrasyonu (canary → prod, hatada durdur).

## 15. Gecelik oto-güncelleme (Faz 3)

Enroll olduktan sonra host, ICLIC'in istediği sürümde kendini tutar — rutin
yükseltme için `deploy-all.sh` gerekmez. Akış **server-authoritative** ve
**privilege-separated**:

1. ICLIC sunucu için bir `desiredAgentVersion` çözer (ring hedefi veya per-server
   pin) ve her heartbeat'te döndürür.
2. Yetkisiz agent bunu `/var/lib/iclic-host-agent/desired-version`'a yazar.
3. **Root** `iclic-host-agent-updater.timer` gecelik **01:00 UTC**'de çalışır
   (`RandomizedDelaySec=900` fleet'i yayar; bozuk release tüm filoyu aynı anda
   düşüremez). `iclic-host-agent-updater` çalışır ve:
   - istenen sürümü kuruluyla karşılaştırır; eşitse çıkar;
   - `install.sh`'i **`STRICT_VERIFY=1`** ile çalıştırır (imza zorunlu — bkz §14);
   - **health-gate:** `HEALTH_TIMEOUT` (180 sn) içinde journal'da `heartbeat
     accepted` görünmeli; görünürse tamam;
   - görünmezse önceki binary'ye **rollback** (`ln -sfn` + restart).

Agent kendini asla güncellemez — sadece direktifi yazar; root timer uygular. Bir
ring'i ilerletmek için operatör ICLIC'te hedef sürümü set eder
(`PUT /api/v1/admin/agent-release-targets/{environment}`); per-server pin
(canary/hold) sunucu kaydında.

```bash
# Host'ta incele / sür
cat /var/lib/iclic-host-agent/desired-version      # ICLIC ne istedi
systemctl list-timers iclic-host-agent-updater     # sonraki çalışma
systemctl start iclic-host-agent-updater.service   # şimdi uygula (01:00'ı bekleme)
journalctl -u iclic-host-agent-updater -n 50       # son çalışma ne yaptı
```

Updater, istenen release'in **imzalı** olmasını şart koşar; doğrulanamayan release
reddedilir (fail-closed) — host, kimliğini doğrulayamadığı bir şeyi kurmaktansa
yerinde kalır. Manuel fleet push için `deploy-all.sh` durur.

---

**Son not:** Bu dokümanın canlı kalması için, her release veya akış değişikliğinde
buraya yansıt. Doküman çürürse yararlı değil.
