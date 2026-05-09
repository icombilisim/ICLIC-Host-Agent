# ICLIC Host Agent — Deploy Rehberi (Aptala Anlatır Gibi)

Bu doküman ICLIC Host Agent'ı release etmek, sunuculara kurmak, yükseltmek
ve rollback yapmak için adım adım bir kılavuzdur. Hiçbir şey ezbere
bilinmesi gerekmiyor — her adımda ne olacağını yazdım.

---

## 1. Sistem Ne Yapıyor?

Her sunucuda küçük bir Go binary çalışıyor (`iclic-host-agent`). Bu binary:

- Her **60 saniyede bir** uyanıyor.
- `/etc/iclic-host-agent/collectors.d/*.yaml` dosyalarını okuyup ne ölçeceğini
  öğreniyor (CPU, RAM, MySQL portu açık mı, ICOSYS servisi sağlıklı mı, vs.).
- Sonuçları **ICLIC sunucusuna POST ediyor** (`/api/v1/server/{id}/heartbeat`).
- O kadar. Hiçbir port açmıyor, hiçbir komut almıyor — sadece push.

ICLIC tarafında bu sinyalleri görmek için:
- **Servers** ekranı → ilgili sunucuya tıkla → host metrikleri ve agent
  versiyonu gözükür.

---

## 2. Mimari — Tek Bakışta

```
   ┌─────────────────────────┐
   │  ICOSYS / DevOps / ICLIC│
   │       sunucusu          │
   │                         │
   │  iclic-host-agent       │
   │  (systemd unit, 60s tick)│
   │           │             │
   │  reads:                 │
   │   /proc, /etc/os-release│
   │   docker.sock, http     │
   │   /etc/iclic-host-agent │
   │   /collectors.d/*.yaml  │
   │           │             │
   │           ▼             │
   │   POST  /api/v1/server/ │
   │        {id}/heartbeat   │
   │   Authorization:        │
   │   Bearer <kid>.<secret> │
   └─────────────┬───────────┘
                 │
                 │  HTTPS
                 ▼
   ┌─────────────────────────┐
   │  ICLIC                  │
   │  Spring Boot @ 8001     │
   │                         │
   │  ServerAgentHeartbeat-  │
   │  Controller             │
   │   ↓ persists            │
   │  Server entity timeline │
   │   ↓ renders             │
   │  Server detail page     │
   └─────────────────────────┘
```

**Önemli:** Agent ICLIC'e gidiyor, ICLIC agent'a hiç gitmiyor. Yani sunucudan
internete sadece **dışa giden** HTTPS yeterli. İçeri açık port gerekmez.

---

## 3. Profiller — Ne Hangi Sunucuya?

Agent, "şu kadar şeyi ölç" listesini YAML dosyalarından alıyor. Her YAML
bir **profil**. Operator (sen) hangi profilleri kuracağına karar veriyorsun.

| Profil | YAML Dosyası | Ne Ölçer |
|---|---|---|
| `host` | `00-linux-host.yaml` | CPU, RAM, disk, uptime, OS, kernel — **HER SUNUCUDA OLMALI** |
| `docker` | `10-docker.yaml` | Docker container sayısı + per-container stats |
| `systemd` | `20-systemd.yaml` | systemd unit'lerin cgroup CPU/MEM kullanımı |
| `icosys` | `30-icosys-actuator.yaml` | 6 ICOSYS Spring Boot servisi (8010-8060) — health/heap/threads |
| `mysql` | `40-mysql.yaml` | MySQL portu açık mı + versiyon |
| `redis` | `50-redis.yaml` | Redis portu + ping + versiyon |
| `nginx` | `60-nginx.yaml` | nginx servisi + 80/443 portları + versiyon |
| `iclic` | `70-iclic.yaml` | ICLIC Spring Boot (port 8001) actuator |
| `devops` | `80-devops-stack.yaml` | Nexus + SonarQube + Dokploy + Postgres |

### Hangi sunucuya hangileri?

| Sunucu | IP / Host | Profiller |
|---|---|---|
| ICOSYS test | <icosys-test-ip> | `host,docker,systemd,icosys,mysql,redis,nginx` |
| ICOSYS prod | <icosys-prod-ip> | `host,docker,systemd,icosys,mysql,redis,nginx` |
| DevOps | <devops-ip> | `host,docker,systemd,devops` |
| ICLIC prod | (Dokploy host) | `host,docker,systemd,iclic` |

**Kural:** Sunucuda olan şeyleri profil olarak ekle, olmayanları ekleme.
DevOps'ta MySQL yoksa `mysql` profili eklemeye gerek yok — agent o port'u
boşuna probe etmesin.

---

## 4. Release Akışı (Maintainer'ın İşi)

Yeni bir versiyon çıkarmak için yapılacaklar:

```
1. Kod değişikliği yap (branch'ta veya main'de)
2. internal/heartbeat/heartbeat.go içindeki AgentVersion sabitini bump et
   (örn: "0.3.0" → "0.4.0")
3. Commit + push
4. Tag at:    git tag v0.4.0 && git push --tags
5. Bekle (~2-3 dk)
6. https://github.com/icombilisim/ICLIC-Host-Agent/releases adresinde
   yeni release görünecek
```

### Arka planda ne oluyor? (`.github/workflows/release.yml`)

```
   ┌─────────────────────────┐
   │  Sen: git push --tags   │
   │  v0.4.0                 │
   └────────────┬────────────┘
                │
                ▼
   ┌─────────────────────────┐
   │  GitHub Actions         │
   │  release.yml tetiklendi │
   │                         │
   │  1. Tag = AgentVersion? │  ← uyuşmazsa fail
   │  2. go vet              │
   │  3. go test             │
   │  4. Build linux-amd64   │
   │  5. Build linux-arm64   │
   │  6. tar configs/        │
   │  7. SHA256SUMS hesapla  │
   │  8. gh release create   │
   └────────────┬────────────┘
                │
                ▼
   ┌─────────────────────────────────────────────┐
   │  GitHub Release v0.4.0 — assets:            │
   │   ├─ iclic-host-agent-linux-amd64           │
   │   ├─ iclic-host-agent-linux-arm64           │
   │   ├─ configs.tar.gz   (tüm YAML profilleri) │
   │   ├─ install.sh                             │
   │   ├─ iclic-host-agent.service               │
   │   └─ SHA256SUMS                             │
   └─────────────────────────────────────────────┘
```

Eğer tag (`v0.4.0`) ile koddaki sabit (`AgentVersion = "0.4.0"`) uyuşmazsa
workflow fail eder — bu kasıtlı. Heartbeat'te yanlış versiyon raporlanması
diye.

---

## 5. İlk Kurulum Akışı (Yeni Sunucuya)

Bir sunucuya **ilk kez** agent kurarken:

```
   ┌─────────────────────────────────────────────┐
   │  1. ICLIC UI → Servers → "New Server"       │
   │     Sunucuyu kaydet, formu doldur.          │
   │     ICLIC sana bir TOKEN verir.             │
   │     (One-shot, TTL'li.)                     │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  2. Sunucuya SSH yap (root veya sudo'lu)    │
   │     ssh icadmin@<icosys-test-ip>                 │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  3. install.sh'i indir + çalıştır:          │
   │                                             │
   │  curl -fsSL https://github.com/icombilisim/ │
   │    ICLIC-Host-Agent/releases/latest/        │
   │    download/install.sh > /tmp/install.sh    │
   │                                             │
   │  sudo TOKEN=<verilen-token> \               │
   │       ICLIC_URL=https://iclic.icombilisim.  │
   │       com \                                 │
   │       PROFILES=host,docker,systemd,icosys,  │
   │                mysql,redis,nginx \          │
   │       bash /tmp/install.sh                  │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  install.sh ne yapıyor:                     │
   │                                             │
   │  a. iclic-agent kullanıcısı oluştur         │
   │  b. /opt/iclic-host-agent dizinleri kur     │
   │  c. Latest release'i indir + SHA256 doğrula │
   │  d. Binary'yi /opt/iclic-host-agent/bin/    │
   │     iclic-host-agent-v0.4.0 olarak yaz      │
   │  e. Symlink: /opt/iclic-host-agent/         │
   │     iclic-host-agent → bin/...v0.4.0        │
   │  f. TOKEN'ı /api/v1/agent/enroll'a POST et  │
   │     → kalıcı bearer al                      │
   │  g. /etc/iclic-host-agent/config.json yaz   │
   │  h. PROFILES'taki YAML'ları collectors.d/   │
   │     içine kopyala                           │
   │  i. systemd unit'i kur ve başlat            │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  4. Doğrula:                                │
   │     systemctl status iclic-host-agent       │
   │     journalctl -u iclic-host-agent -f       │
   │                                             │
   │  60 sn içinde ICLIC dashboard'unda:         │
   │     enrollment_status: PENDING_ENROLLMENT   │
   │                  →    HEALTHY               │
   └─────────────────────────────────────────────┘
```

**Token notu:** Token tek kullanımlık. Eğer install.sh enroll adımında
hata alırsa, TOKEN yenisi olmadan tekrar denersen ICLIC reddeder. Bu
durumda ICLIC'ten yeni bir token üret ve baştan başla.

---

## 6. Upgrade Akışı (Tek Sunucu — Re-run install.sh)

Sunucu zaten enroll olmuş, sadece yeni versiyona geçiyoruz:

```
   ┌─────────────────────────────────────────────┐
   │  ssh icadmin@<icosys-test-ip>                    │
   │                                             │
   │  curl -fsSL https://github.com/icombilisim/ │
   │    ICLIC-Host-Agent/releases/latest/        │
   │    download/install.sh > /tmp/install.sh    │
   │                                             │
   │  sudo bash /tmp/install.sh                  │
   │                                             │
   │  (TOKEN gerekmiyor — config.json zaten var) │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  install.sh:                                │
   │  - config.json varlığını fark etti          │
   │  - "upgrade mode" yazdı                     │
   │  - Yeni binary'yi indirdi                   │
   │  - SHA256 doğruladı                         │
   │  - bin/iclic-host-agent-v0.5.0 olarak yazdı │
   │  - Symlink retarget: → v0.5.0               │
   │  - systemctl restart iclic-host-agent       │
   └─────────────────────────────────────────────┘
```

Önceki binary diskte kalır (`bin/iclic-host-agent-v0.4.0`) — rollback
gerektiğinde lazım olur.

---

## 7. Fleet Deploy (Tüm Sunucular Birden — `deploy-all.sh`)

`deploy-all.sh` SSH ile sırayla her sunucuya bağlanıp install.sh'i çalıştırır.

### Hazırlık (sadece bir kez)

```bash
cd D:\projectMavenGIT\ICLIC-Host-Agent\installer\
cp inventory.example inventory.local
notepad inventory.local
```

`inventory.local` içeriği şöyle olur (gerçek IP'ler, gerçek user'lar):

```
<icosys-test-ip>:host,docker,systemd,icosys,mysql,redis,nginx:icadmin
<icosys-prod-ip>:host,docker,systemd,icosys,mysql,redis,nginx:icadmin
<devops-ip>:host,docker,systemd,devops:icadmin
```

### Çalıştırma

```bash
bash deploy-all.sh inventory.local v0.4.0
```

### Akış

```
   ┌─────────────────────────────────────────────┐
   │  Sen: bash deploy-all.sh inv.local v0.4.0   │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  Inventory'i satır satır oku.               │
   │  Her satır için (paralel DEĞİL — sırayla):  │
   │                                             │
   │   1. scp install.sh → /tmp/                 │
   │   2. ssh + sudo bash install.sh             │
   │      AGENT_VERSION=v0.4.0                   │
   │      PROFILES=<inventory'deki>              │
   │   3. Sonucu kaydet (✓ veya ✗)               │
   │   4. Bir sonraki satıra geç                 │
   │                                             │
   │  Bir sunucu fail ederse loop devam eder.    │
   └────────────────────┬────────────────────────┘
                        │
                        ▼
   ┌─────────────────────────────────────────────┐
   │  Sonunda özet:                              │
   │   succeeded: 2                              │
   │     ✓ <icosys-test-ip>                           │
   │     ✓ <icosys-prod-ip>                        │
   │   failed: 1                                 │
   │     ✗ <devops-ip>                          │
   │                                             │
   │  Exit code = fail sayısı (0 = hepsi başarı) │
   └─────────────────────────────────────────────┘
```

**Önkoşul:** SSH erişimin password-less olmalı (key-based auth) ve hedef
sunucularda `sudo -n bash install.sh` çalışmalı (sudoers'ta NOPASSWD).
Aksi halde script ortada takılır.

**Önemli:** `deploy-all.sh` SADECE upgrade içindir. İlk kurulum (TOKEN gerektiren)
her sunucuda elle yapılır — token sunucu başına farklı çünkü.

---

## 8. Rollback Akışı

Yeni versiyon bozuk çıkarsa, eski binary diskte zaten duruyor — sadece
symlink'i geri al:

```bash
ssh icadmin@<icosys-test-ip>

# Hangi versiyonlar var bakalım:
ls /opt/iclic-host-agent/bin/
# çıktı:
#   iclic-host-agent-v0.3.0
#   iclic-host-agent-v0.4.0   (şu an bozuk)

# Eski versiyona geri dön:
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.3.0 \
              /opt/iclic-host-agent/iclic-host-agent

sudo systemctl restart iclic-host-agent

# Doğrula:
journalctl -u iclic-host-agent -n 50
```

Bu işlem **5 saniye** sürer ve config'e dokunmaz.

Eğer fleet'in tamamında rollback gerekirse: `deploy-all.sh inv.local v0.3.0`
çalıştır — install.sh idempotent olduğu için eski versiyona "yükseltir"
(yani geri döner).

---

## 9. Doğrulama — Kuruldu mu, Çalışıyor mu?

### Sunucu tarafında

```bash
# Servis çalışıyor mu
systemctl status iclic-host-agent

# Log'da hata var mı (60s'lik tick'leri görmek için):
journalctl -u iclic-host-agent -f

# Hangi YAML'lar aktif:
ls /etc/iclic-host-agent/collectors.d/

# Hangi versiyon yüklü:
ls -la /opt/iclic-host-agent/iclic-host-agent
# çıktı: ... iclic-host-agent → /opt/iclic-host-agent/bin/iclic-host-agent-v0.4.0
```

### ICLIC tarafında

ICLIC dashboard:

1. **Servers** ekranı → sunucuyu bul
2. `last_seen_at` alanı 60 saniyeden daha taze olmalı
3. `agent_version` alanı `v0.4.0` (veya yüklediğin tag) olmalı
4. **Server Detail** sayfası → "Host Metrics" paneli → CPU/RAM/disk
   değerleri gerçek olmalı
5. "Heartbeat History" panel'de raw payload'ı görebilirsin — eklediğin
   profil'in key'leri (`mysql_running`, `nginx_version`, vs.) orada
   gözükmeli

---

## 10. Sorun Giderme

### "install.sh fail oldu, sunucu hala enroll değil"

- Token expired mi? — ICLIC'ten yeni bir tane üret.
- Network: sunucudan `curl https://iclic.icombilisim.com/actuator/health`
  cevap veriyor mu?
- DNS: `dig iclic.icombilisim.com` — IP dönüyor mu?

### "systemctl status active ama ICLIC heartbeat görmüyor"

- `journalctl -u iclic-host-agent -n 100` — hangi hatayı gördüğüne bak.
- `cat /etc/iclic-host-agent/config.json` — `iclic_url` doğru mu?
- ICLIC'in `enrollment_status` `PENDING_ENROLLMENT`'da kalıyorsa, yeniden
  enroll etmek lazım — bu durumda `config.json`'ı sil + yeni TOKEN ile
  install.sh'i tekrar çalıştır.

### "Bir collector key gözükmüyor"

- O profil `/etc/iclic-host-agent/collectors.d/` içine yüklendi mi?
- YAML primitive'i o sunucuda gerçekten çalıştırılabilir mi? Örn:
  `nginx -v` komutu PATH'te yok mu? `redis-server --version` yok mu?
- Log'da WARN olarak gözükür: `journalctl -u iclic-host-agent | grep WARN`

### "deploy-all.sh ortada takıldı"

- SSH key-based auth çalışıyor mu? `ssh -o BatchMode=yes <host>` direkt
  prompt görmeden bağlanmalı.
- Hedef sunucuda `sudo -n true` hatasız çalışmalı (NOPASSWD lazım).

### "SHA256 mismatch"

- GitHub release tampered olmuş olabilir (çok düşük ihtimal) veya indirme
  yarıda kaldı — install.sh'i tekrar çalıştır.

---

## 11. Hızlı Komut Sözlüğü

```bash
# Yeni release çıkar
git tag v0.4.0 && git push --tags

# Tek sunucuya ilk kurulum
sudo TOKEN=xyz ICLIC_URL=https://iclic.icombilisim.com \
     PROFILES=host,docker,systemd,icosys bash install.sh

# Tek sunucuda upgrade
sudo bash install.sh

# Tek sunucuda spesifik versiyona git (downgrade dahil)
sudo AGENT_VERSION=v0.3.0 bash install.sh

# Fleet upgrade
bash deploy-all.sh inventory.local v0.4.0

# Tek sunucuda rollback
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.3.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent

# Verify
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f
ls /etc/iclic-host-agent/collectors.d/
```

---

## 12. Dosya Konumları (Cheat Sheet)

| Yer | İçerik |
|---|---|
| `/opt/iclic-host-agent/iclic-host-agent` | Symlink → aktif binary |
| `/opt/iclic-host-agent/bin/iclic-host-agent-vX.Y.Z` | Versiyonlu binary'ler |
| `/etc/iclic-host-agent/config.json` | Enrolment kimlik bilgileri (root:iclic-agent 0640) |
| `/etc/iclic-host-agent/collectors.d/` | Aktif YAML profilleri |
| `/var/lib/iclic-host-agent/state.json` | Agent state (heartbeat sequence vs) |
| `/etc/systemd/system/iclic-host-agent.service` | systemd unit |

---

**Son not:** Bu dokümanın canlı kalması için, her release veya akış
değişikliğinde buraya yansıt. Doküman çürürse yararlı değil.
