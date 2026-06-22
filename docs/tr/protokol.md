# ICLIC Host Agent → ICLIC Heartbeat Protokolü

> **Sürüm** v0.15.0 · **Son güncelleme** 2026-06-22 · **Kanonik dil** İngilizce
> · [ICLIC Host Agent dokümanları](../README.md) bütününün parçası
>
> **protocolVersion:** 1 — alan kümesi her değişiklikten önce büyüyebilir; kırıcı
> değişiklikte `protocolVersion`'ı bump edin.

## Taşıma (transport)

- Yalnızca HTTPS.
- Bir ajan → bir ICLIC URL'i (kurulumda yapılandırılır).
- Ajan yalnızca gönderir; ICLIC ajana hiç bağlanmaz. İzlenen host'ta içeri açık
  port yoktur.

## Endpoint'ler

### Enrollment (tek seferlik)

```
POST {ICLIC_URL}/api/v1/agent/enroll
Content-Type: application/json
```

```json
{
  "token": "<one-shot-bootstrap-token>",
  "label": "agent on api-01"
}
```

Token bir ICLIC admin tarafından verilir (TTL'li, tek kullanımlık). Path
herkese açıktır — token'ın kendisi kimlik bilgisidir. Başarılı yanıt
`server_id`, `agent_kid` ve `agent_secret`'ı tam olarak bir kez döndürür;
installer bunları yerelde saklar ve secret yeniden verilemez.

### Heartbeat

```
POST {ICLIC_URL}/api/v1/server/{serverId}/heartbeat
Content-Type:  application/json
Authorization: Bearer <kid>.<secret>
User-Agent:    iclic-host-agent/<version>
```

Path'teki `serverId`, gövdedeki `server_id` alanıyla eşleşir — path ICLIC
yönlendirme/loglama için vardır, ajanın kendi raporladığı şey gövde alanıdır.

## Kimlik doğrulama

PAT-tarzı bearer:

```
Authorization: Bearer <kid>.<secret>
```

ICLIC ilk `.`'ten böler ve secret yarısını kid'e karşılık saklanan SHA-256
özetine göre doğrular. TLS, kabloda gizlilik sağlar — per-request imza yoktur.

Aynı bearer şeması kimlik doğrulamalı her `/api/v1/server/**` endpoint'i için
(heartbeat, runtime-instances ve `wss://…/control` kanalı) kullanılır; bu yüzden
ajanın çağrı başına ayrı kimlik bilgisine ihtiyacı yoktur.

### Runtime versiyon sinyalleri

```
POST {ICLIC_URL}/api/v1/server/runtime-instances/heartbeat
Content-Type:  application/json
Authorization: Bearer <kid>.<secret>
User-Agent:    iclic-host-agent/<version>
```

Ajan, host heartbeat'i kabul edildikten sonra `metrics.runtime_instances`
altında bulunan her öğe için bu endpoint'i bir kez çağırır. Hatalar öğe başına
loglanır ama host heartbeat'ini başarısız yapmaz.

```json
{
  "productCode": "ICOSYS",
  "componentCode": "hrm-backend",
  "instanceKey": "prod-api-01:hrm-backend",
  "environment": "PROD",
  "status": "HEALTHY",
  "versionSource": "HOST_AGENT",
  "runningVersion": "1.21.1",
  "gitCommit": "abc1234",
  "payload": { "source": "systemd", "unit": "icosys-hrm.service" }
}
```

Host-agent kimlik bilgileri için ICLIC sinyali kimlik doğrulamalı sunucuya
bağlar ve `runtimeComponentId` verilmedikçe `productCode` + `componentCode`
ister. Installation kimlik bilgileri de aynı endpoint'i çağırabilir; ICLIC o
zaman yazmaları installation'ın kendi ürünüyle kısıtlar.

### Neden request-signing değil?

Erken taslaklar kanonik bir request string'i üzerinden HMAC-SHA256 kullanıyordu.
Bunun yerine düz bearer-over-TLS seçildi çünkü:

- ICLIC'in mevcut PAT şeması (installation→authority çağrıları) zaten bearer
  formunu kullanıyor; kimlik tipini paylaşmak doğrulayıcı kod yollarını tek tip
  tutuyor.
- TLS oturum penceresinin ötesinde replay savunmasının somut bir hasmı yok —
  heartbeat'ler komut değil, idempotent state-overwrite.
- Sızan bir bearer mevcut anahtar iptal akışıyla kurtarılabilir.

Gelecekte bir endpoint gerçekten replay-proof anlamlar gerektirirse üzerine bir
nonce ekleyebilir — ama host-izleme yüzeyinin böyle bir ihtiyacı yok.

## Payload v1

Wire zarfı üst seviyede **camelCase**'tir (ICLIC'in varsayılan Jackson
adlandırmasıyla uyumlu) ve altında serbest formlu bir `metrics` map vardır. Ajan,
ICLIC tarafında şema değişikliği olmadan zamanla yeni metrik key'leri ekler.

`metrics` gövdesi **toplayıcı hattı** tarafından üretilir — operatör tarafındaki
YAML şeması için [`toplayicilar.md`](toplayicilar.md). Aşağıdaki key'ler
varsayılan `00-linux-host.yaml` profilinin ürettikleridir; operatörler ajan kod
değişikliği olmadan ekleyebilir (veya çıkarabilir).

```json
{
  "agentVersion": "0.15.0",
  "protocolVersion": 1,
  "metrics": {
    "reported_at": "2026-06-22T12:34:56Z",
    "status": "UP",
    "hostname": "api-01",
    "uptime_seconds": 1234567,
    "os_name": "ubuntu",
    "os_version": "24.04",
    "kernel": "6.8.0-31-generic",
    "arch": "amd64",
    "cpu_count": 4,
    "cpu_used_pct": 12.5,
    "load_1m": 0.45,
    "load_5m": 0.31,
    "load_15m": 0.20,
    "mem_used_pct": 48.2,
    "mem_total_mb": 16384,
    "mem_used_mb": 7900,
    "mem_available_mb": 8484,
    "disks": [
      { "mount": "/",                "used_pct": 22.0, "total_gb": 100 },
      { "mount": "/var/lib/docker",  "used_pct": 56.0, "total_gb": 500 }
    ],
    "disk_used_pct_max": 56.0,
    "os_security_updates_pending": 0,
    "reboot_required": false
  }
}
```

### Neden kabloda iki ayrı case?

- `agentVersion` ve `protocolVersion`, ICLIC'in özel olarak işlediği (badge
  render, versiyon-skew tespiti) **tipli zarf alanlarıdır**. Jackson'ın
  varsayılan camelCase'ini takip eder, ICLIC API yüzeyinin geri kalanıyla aynı.
- `metrics` içindeki her şey **ICLIC için opak'tır** — `server.host_metrics_json`
  içinde aynen JSON olarak saklanır ve `server_heartbeat_history`'ye kopyalanır.
  Ajan orada snake_case kullanır çünkü kaynak verinin kendisi (`/proc/*`,
  `os-release`) snake_case'tir; yeniden adlandırmak tüketici olmadan sürtünme
  yaratırdı.

### Alan notları

| Alan | Notlar |
|------|--------|
| `protocolVersion` | Tam sayı. Kırıcı değişiklikte bump edilir. ICLIC son N versiyonu kabul eder. |
| `agentVersion` | Serbest form. ICLIC'in "agent outdated" badge'leri için; load-bearing değil. |
| `metrics.status` | `UP` \| `DEGRADED` — ajanın kendi değerlendirmesi. Varsayılan `UP`; bir binding override edebilir. ICLIC, kaçan heartbeat'lerde sunucu tarafında `STALE`'e çevirir. |
| `metrics.cpu_used_pct` | Host CPU kullanımı %, metrik geçmişi için v0.13.0'da eklendi. |
| `metrics.disks[]` | Her gerçek mount için bir öğe; pseudo dosya sistemleri varsayılan olarak hariç. Boş dizi geçerli. |
| `metrics.disk_used_pct_max` | `disks[]` içindeki en yüksek `used_pct`; backend `buildSummary` bunu doğrudan okur. |
| `metrics.os_security_updates_pending` | `-1` = "ajan belirleyemedi" (apt kilitli, dnf primitive olmayan RHEL host vb.) |
| `metrics.reported_at` | Örnekleme anındaki ajan tarafı duvar saati. ICLIC kendi `received_at`'ini de damgalar; ikisi ayrı tutulur ki clock skew gözlemlenebilsin. |
| Diğer her şey | Serbest form — operatör tanımlı binding'ler bildirdikleri key'leri üretir. Backend tam payload'ı `server_heartbeat_history.payload_json`'da aynen saklar. |

## Versiyonlama kuralları

- **Eklemeli değişiklik** (yeni opsiyonel alan) → versiyon bump yok. Eski ajanlar
  çalışmaya devam eder; ICLIC eksik alanlara tolerans gösterir.
- **Kırıcı değişiklik** (rename, kaldırma, tip değişimi) → `protocolVersion`'ı
  bump et. ICLIC önceki versiyonun parser'ını en az bir major release boyunca
  tutar.
- Ajan her zaman bildiği en yeni versiyonu gönderir. ICLIC ajandan asla düşürme
  istemez — ajan çok yeniyse ICLIC `400` döner ve operatör ICLIC'i yükseltmelidir.

## Hatalar

| Durum | Anlamı | Ajan tepkisi |
|-------|--------|--------------|
| 200 / 204 | Kabul edildi | Devam |
| 400 | Bozuk payload, desteklenmeyen `protocolVersion` veya path serverId bearer ile uyuşmuyor | Logla + sonraki tick'te tekrar dene (backoff yok) |
| 401 | Bearer eksik, süresi dolmuş, iptal veya bilinmiyor | Logla + tekrar dene; kalıcıysa sysadmin yeniden enroll etmeli |
| 403 | Sunucu `enrollment_status` = DISABLED | Dur ve non-zero çık ki systemd hatayı yüzeye çıkarsın |
| 5xx | ICLIC down | Logla + sonraki tick'te tekrar dene |

Ajan exponential backoff uygulamaz; systemd `Restart=on-failure` politikası ve
sabit 60 saniyelik tick kasıtlı olarak tek yeniden deneme mekanizmasıdır.
