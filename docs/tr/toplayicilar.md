# Operatör tanımlı toplayıcılar (collectors)

> **Sürüm** v0.20.0 · **Son güncelleme** 2026-06-25 · **Kanonik dil** İngilizce
> · [ICLIC Host Agent dokümanları](../README.md) bütününün parçası

Ajanın metrik gövdesi `/etc/iclic-host-agent/collectors.d/` içindeki bir veya
birden çok YAML dosyasından oluşur. Her dosya düz bir *binding* listesidir; her
binding bir built-in *primitive* adı verir, argümanlarını sağlar ve sonucun
heartbeat'in `metrics` map'inde hangi key altına düşeceğini bildirir.

Dosyalar alfabetik okunur ve her tick'te tek bir binding listesine birleştirilir
— bir dosyayı düzenlemek, eklemek veya kaldırmak ajan restart'ı gerektirmez.

> **Not:** Bu projede alan/kolon adları İngilizcedir. Aşağıdaki `primitive`
> adları, `args` anahtarları ve `output_key` değerleri olduğu gibi kullanılır;
> yalnızca açıklamalar Türkçedir.

## Bir binding'in anatomisi

```yaml
- id: cpu_load_1m            # insan etiketi, yalnızca hata loglarında kullanılır
  primitive: procfs.loadavg  # aşağıdaki built-in'lerden biri
  args: { window: 1m }       # primitive'e aynen iletilir
  output_key: load_1m        # sonucun metrics{} altında konacağı key
```

`primitive: <unknown>` olan, `output_key`'i eksik veya `id`'si boş binding'ler
bir uyarı ile atlanır. Hata döndüren bir primitive WARN'da loglar ve o tick için
metriğini atlar — asla ajanı çökertmez.

## Built-in primitive'ler

### procfs.loadavg

`/proc/loadavg` okur. Bir sayı döndürür (istenen pencere için load ortalaması).

| Arg    | Tip    | Varsayılan | Açıklama |
|--------|--------|------------|----------|
| window | string | `1m`       | `1m` / `5m` / `15m` |

### procfs.uptime

`/proc/uptime` okur. Kernel uptime'ını tam saniye olarak döndürür (int).

### procfs.memory

`/proc/meminfo` okur. "Used", modern Linux konvansiyonunu izler
(`MemTotal - MemAvailable`) ki cache sayfaları sayıyı şişirmesin.

| Arg   | Tip    | Varsayılan | Açıklama |
|-------|--------|------------|----------|
| field | string | `used_pct` | `used_pct` / `total_mb` / `used_mb` / `free_mb` / `available_mb` |

### procfs.swap

`/proc/meminfo` (`SwapTotal` / `SwapFree`) okur. Swap'i olmayan host `used_pct`
için `0` raporlar ki metrik her zaman bulunsun.

| Arg   | Tip    | Varsayılan | Açıklama |
|-------|--------|------------|----------|
| field | string | `used_pct` | `used_pct` / `total_mb` / `used_mb` |

### procfs.cpu_count

Online mantıksal CPU sayısını döndürür (int).

### procfs.cpu_used_pct

`/proc/stat`'ı iki kez örnekler ve host CPU kullanımını yüzde olarak döndürür
(float). ICLIC'in metrik geçmişini beslemek için v0.13.0'da eklendi.

### procfs.diskstats

`/proc/diskstats`'ı iki kez örnekler ve gerçek (tam) diskler genelinde toplam
disk I/O oranlarını döndürür: `{ read_mbps, write_mbps, iops }`.

| Arg        | Tip    | Varsayılan | Açıklama |
|------------|--------|------------|----------|
| sample_sec | number | `1`        | Örnekleme penceresi, 0.2..5 aralığına sıkıştırılır |

### procfs.netdev

`/proc/net/dev`'i iki kez örnekler ve gerçek arayüzler genelinde toplam ağ
oranlarını döndürür: `{ rx_mbps, tx_mbps, rx_errors, tx_errors }`.

| Arg        | Tip    | Varsayılan | Açıklama |
|------------|--------|------------|----------|
| sample_sec | number | `1`        | Örnekleme penceresi, 0.2..5 aralığına sıkıştırılır |
| iface      | string | (tümü)     | Tek bir arayüzle sınırla; aksi halde tüm sanal-olmayanlar |

### os.release

`/etc/os-release` okur. Tek bir alanı string olarak döndürür.

| Arg   | Tip    | Varsayılan | Açıklama |
|-------|--------|------------|----------|
| field | string | `version`  | `id` / `name` / `version` / `version_id` / `pretty_name` |

### os.hostname

Kernel'in bildirdiği hostname'i döndürür (string).

### os.kernel

Çalışan kernel sürümünü döndürür (string, örn. `6.8.0-31-generic`).

### os.arch

Ajan binary'sinin GOARCH'ını döndürür (string, `amd64` / `arm64`).

### reboot.required

`/var/run/reboot-required` varsa `true` döndürür (bool).

### disk.usage

`df -kP` çağırır. Pseudo dosya sistemleri (tmpfs, overlay, …) `exclude_pseudo:
false` verilmedikçe atılır.

| Arg            | Tip    | Varsayılan | Açıklama |
|----------------|--------|------------|----------|
| mount          | string | (unset)    | Tek mount → bir map döner. Verilmezse → tüm gerçek mount'ların listesi. |
| exclude_pseudo | bool   | `true`     | tmpfs / overlay / squashfs / udev / sysfs / cgroup / proc / `none` atılır |

Mount başına şekil: `{mount: "/", used_pct: 22.0, total_gb: 100}`.

### disk.max_used_pct

`disk.usage` ile aynı kaynak ama tek sayıya indirgenmiş — tüm gerçek mount'lar
arasındaki en yüksek `used_pct`. Backend bunu Sunucular listesindeki tek satırlık
"disk=72%" özeti için kullanır.

| Arg            | Tip  | Varsayılan | Açıklama |
|----------------|------|------------|----------|
| exclude_pseudo | bool | `true`     | `disk.usage` ile aynı |

### exec

Keyfi bir komut çalıştırır. Kaçış kapısı — adlı bir primitive ile kapsanmayan her
şey (WildFly CLI, jboss-cli, özel script'ler) buradan geçer.

| Arg         | Tip      | Varsayılan | Açıklama |
|-------------|----------|------------|----------|
| cmd         | []string | —          | argv-tarzı; shell expansion yok |
| timeout_sec | number   | `5`        | 30'da kapanır |
| parse       | string   | `raw`      | `raw` / `trimmed` / `int` / `float` / `json` |
| path        | string   | (unset)    | Yalnızca `parse: json` ile — decode edilmiş dokümandan dotted path. `http.get_json` ile aynı söz dizimi. |

Sıfırdan farklı çıkış hata sayılır (o tick metrik atlanır).

### systemctl.is_active

`systemctl is-active <unit>` `active` raporladığında `true` döndürür (bool).

| Arg  | Tip    | Varsayılan | Açıklama |
|------|--------|------------|----------|
| unit | string | —          | örn. `nginx.service` |

### systemd.resources

Bir veya birden çok unit için cgroup tabanlı CPU + bellek + restart sayaçlarını
`systemctl show -p Id,ActiveState,SubState,LoadState,MainPID,MemoryCurrent,CPUUsageNSec,NRestarts`
ile okur. server-detail "Services" paneli tek bir tablo render edebilsin diye
istenen unit başına bir map'lik liste döndürür.

| Arg         | Tip      | Varsayılan | Açıklama |
|-------------|----------|------------|----------|
| units       | []string | —          | Tam unit adları, örn. `icosys-icglb.service` |
| timeout_sec | number   | `4`        | Binding başına tavan; unit başına bir fork+exec |

Unit başına şekil:

```
{ unit, id, active_state, sub_state, load_state, main_pid,
  memory_mb, cpu_ns, n_restarts }
```

`cpu_ns` kümülatif `CPUUsageNSec` sayacıdır — backend heartbeat'ler arası yüzde
türetir. Eksik unit'ler satırı düşürmek yerine `load_state: not-found` ile gelir;
böylece UI kaldırılmış servisler için bile sabit bir slot tutar.

### tcp.connect

TCP bağlantısı timeout içinde tamamlanırsa `true` döndürür (bool).

| Arg         | Tip    | Varsayılan | Açıklama |
|-------------|--------|------------|----------|
| host        | string | —          | DNS adı veya IP |
| port        | number | —          | 1..65535 |
| timeout_sec | number | `2`        | |

### http.get

Tek bir GET isteği.

| Arg         | Tip    | Varsayılan | Açıklama |
|-------------|--------|------------|----------|
| url         | string | —          | Tam URL |
| timeout_sec | number | `3`        | |
| expect      | string | `code`     | `code` (status int döner) / `ok` (200..299 bool döner) |

### http.probe

Sentetik uptime kontrolü: tek bir GET; erişilebilirlik, gecikme ve durumu
`{ up, latency_ms, status }` map'i olarak raporlar. Bağlantı hatası, hata yerine
`up: false` (`status: 0`) döndürür — yani "down" eksik metrik değil, kaydedilmiş
bir veri noktasıdır. `up`, varsayılan olarak 2xx/3xx için `true`'dur veya
`expect_status` verilmişse tam olarak o koddur.

| Arg           | Tip    | Varsayılan | Açıklama |
|---------------|--------|------------|----------|
| url           | string | —          | Tam URL |
| timeout_sec   | number | `5`        | |
| expect_status | number | (unset)    | Verilmişse `up` tam olarak bu status kodunu ister |

### http.get_json

Bir JSON dokümanı çeker ve ya tüm gövdeyi ya da dotted path ile çıkarılan tek bir
değeri döndürür. Bir Spring Boot actuator endpoint'ini, Nexus admin API'sini veya
başka herhangi bir JSON şeklindeki admin yüzeyini per-target toplayıcı yazmadan
kazımanın en ucuz yolu.

| Arg         | Tip    | Varsayılan | Açıklama |
|-------------|--------|------------|----------|
| url         | string | —          | Tam URL |
| path        | string | (unset)    | Dotted path; boş = tüm dokümanı döndür |
| header      | map    | (unset)    | Ek istek başlıkları (`Authorization` vb.) |
| basic_user  | string | (unset)    | HTTP Basic auth için `basic_pass` ile eşle |
| basic_pass  | string | (unset)    | |
| timeout_sec | number | `4`        | |

Path söz dizimi:

- `status` — üst seviye key
- `components.db.status` — iç içe key'ler
- `measurements.0.value` — sayısal segment bir diziye index'ler

Sayılar ve boolean'lar `float64` / `bool`, string'ler `string` olarak döner.
Eksik key'ler `nil` üretir ve binding'in metriği atlanır. Yanıt gövdesi, kötü
davranan bir endpoint'in bir tick'i patlatmasını önlemek için 1 MB'da kapanır.

### ssl.cert_expiry

`host:port`'a TLS ile bağlanır ve leaf (sunucu) sertifikası dolana kadar kalan
tam gün sayısını döndürür (int). `90-tls.yaml` profilini besler ki ICLIC bir
sertifika dolmadan uyarı verebilsin.

| Arg         | Tip    | Varsayılan | Açıklama |
|-------------|--------|------------|----------|
| host        | string | —          | Bağlanılacak hedef (zorunlu) |
| port        | number | `443`      | 1..65535 |
| server_name | string | (host)     | Sertifikayı seçmek için SNI; varsayılanı `host` |
| timeout_sec | number | `5`        | |

### docker.containers

`docker` CLI gerekmeden doğrudan `/var/run/docker.sock` üzerinden `dockerd` ile
konuşur ve container başına bir satır + bir state-bucket özeti döndürür. Ajan, bu
binding'i taşıyan her host'ta `docker` grubunun üyesi olmalı —
`installer/install.sh` bunu otomatik yapar.

| Arg         | Tip    | Varsayılan             | Açıklama |
|-------------|--------|------------------------|----------|
| socket      | string | `/var/run/docker.sock` | Unix socket yolu |
| all         | bool   | `true`                 | Durmuş container'ları dahil et |
| timeout_sec | number | `4`                    | |

Şekil:

```
{
  total, running, exited, restarting, paused, dead, created, removing,
  list: [ { name, image, state, status, restart_count } ]
}
```

### docker.stats

Container başına CPU + bellek anlık görüntüsü; docker `stats` endpoint'inden
`stream=false` ile çekilir. CPU%, `docker stats`'ın hesapladığı gibi hesaplanır —
`cpu_usage.total_usage`'ın `system_cpu_usage` üzerindeki deltası, online CPU ile
çarpılır — yani her çekirdeği %100'de olan 2 çekirdekli bir kutu `200.0` raporlar.

| Arg         | Tip    | Varsayılan             | Açıklama |
|-------------|--------|------------------------|----------|
| socket      | string | `/var/run/docker.sock` | Unix socket yolu |
| timeout_sec | number | `6`                    | Container başına timeout; goroutine'ler fan-out yapar, böylece binding'in toplamı sınırlı kalır |

Şekil: şunlardan liste

```
{ name, image, state, cpu_pct, mem_used_mb, mem_limit_mb, mem_pct,
  restart_count }
```

Tüm docker primitive'leri ve tick'ler arasında tek, process-ömrü boyunca bir HTTP
client paylaşılır (v0.4.0'da eklendi). Önceki build'ler her istekte taze bir
`http.Transport` açıyordu ve uzun çalışan host'larda bellek sızdırıyordu. (#2)

### runtime.services

Yapılandırılabilir bir servis kayıt defterini ICLIC Fleet ve Deployment Status
için `runtime_instances` sinyallerine çevirir. Tek seferlik `exec` probe'larının
aksine bu primitive yapılandırılan servis başına her zaman bir satır üretir:
başarısız bir container veya actuator probe'u tanı payload'ı ile `STALE` olur,
böylece UI satırı kaybetmek yerine bozuk servisi gösterir. (#112)

| Arg         | Tip    | Varsayılan             | Açıklama |
|-------------|--------|------------------------|----------|
| socket      | string | `/var/run/docker.sock` | Container durumunu denetlemek için kullanılan Docker socket |
| timeout_sec | number | `4`                    | Servis başına probe timeout |
| services    | []map  | —                      | Servis kayıt girdileri |

Servis girdisi alanları:

| Alan            | Zorunlu | Açıklama |
|-----------------|---------|----------|
| product_code    | evet    | ICLIC ürün kodu, örn. `ICOSYS` |
| component_code  | evet    | `runtime_component.code`, örn. `icglb-services` |
| health_url      | evet    | JSON health endpoint |
| info_url        | hayır   | JSON info/version endpoint |
| container       | hayır   | Denetlenecek Docker container adı |
| probe           | hayır   | `http` veya `docker_exec`; `docker_exec`, `container` içinde `wget` çalıştırır |
| instance_key    | hayır   | Kararlı kimlik; varsayılanı container veya component code |
| environment     | hayır   | Biliniyorsa `PROD`, `TEST`, `STAGING` veya `DEV` |
| version_path    | hayır   | Versiyon için JSON dot path; varsayılanı `app.version`, sonra `build.version` |
| git_commit_path | hayır   | Commit için JSON dot path; varsayılanı `git.commit.id` |

**Versiyon kaynağı.** `container` ayarlıysa çalışan versiyon, container'ın OCI image
label'ından **`org.opencontainers.image.version`** alınır — build anında basılan ve
promote-by-retag boyunca korunan kanonik release versiyonu (mutable tag'de değil,
image config'inde durur). Bu, run-state için yapılan aynı container inspect'inden
okunur; ekstra Docker API çağrısı yok, servis-başına config gerektirmez. Image'da bu
label yoksa (label'sız / ICOM-dışı imajlar) actuator `info` dokümanına düşer
(`version_path` → `app.version` → `build.version`). `com.icom.image.rc` label'ı
varsa `buildRef` olarak raporlanır (RC provenance; test satırlarında versiyondan
ayrı gösterilir). (#55)

Örnek:

```yaml
- id: icosys_runtime_instances
  primitive: runtime.services
  args:
    services:
      - product_code: ICOSYS
        component_code: icglb-services
        container: icosys-icglb
        probe: docker_exec
        health_url: http://127.0.0.1:8010/icglb/services/actuator/health
        info_url: http://127.0.0.1:8010/icglb/services/actuator/info
  output_key: runtime_instances
```

Servis eklemek bir katalog + config işlemidir: ICLIC'te eşleşen
`runtime_component` satırını oluştur veya aktive et, host'un collector YAML'ına
bir servis girdisi ekle, sonra bir sonraki heartbeat'i bekle. Çıkarmak tersi:
servis girdisini sil veya katalog component'ini inaktif işaretle.

### file.stat

| Arg   | Tip    | Varsayılan | Açıklama |
|-------|--------|------------|----------|
| path  | string | —          | |
| field | string | `exists`   | `exists` (bool) / `size_bytes` (int) / `mtime_seconds` (int) / `age_seconds` (int, son değişiklikten beri saniye). Yol yoksa sayısal alanlar `-1` döner. |

### file.newest_age_seconds

Bir glob ile eşleşen en son değiştirilmiş normal dosyadan beri geçen saniyeyi
döndürür — yani "en yeni yedek ne kadar eski". Hiçbir şey eşleşmezse `-1` döner.
Dosya adının her çalıştırmada değiştiği zaman damgalı dump dizinleri için yapıldı;
yaş yedek aralığını aştığında uyar (örn. günlük dump için `> 93600` = 26 saat).

| Arg  | Tip    | Varsayılan | Açıklama |
|------|--------|------------|----------|
| glob | string | —          | örn. `/opt/iclic/mysql/backups/*.sql.gz` |

### apt.security_count

`apt-get -s upgrade` parse ederek bekleyen güvenlik güncellemelerini sayar. int
döner. RHEL/CentOS'ta veya apt kilitli / eksik / timeout olduğunda `-1` döner —
"ajan belirleyemedi" için belgeli sentinel. Asla hata vermez ki Debian olmayan
host'lar, bir `dnf.security_count` primitive'i çıkana dek doğru "bilinmiyor"
sinyalini alsın.

### security.snapshot

Haftalık fleet güvenlik digest'i için bileşik güvenlik telemetrisi: WAF /
ModSecurity blokları, nginx 4xx, fail2ban banları ve firewall drop'ları — hepsi
backend'in sunucu başına sakladığı iç içe `security_snapshot` nesnesinde. Her alt
kaynak yoksa **kendini atlar** (eksik container, okunamayan log, yetki yok), bu
yüzden binding her host'ta güvenli ve yeni sunucu config'siz uyum sağlar.

Her heartbeat'te log taramamak için en çok `window_seconds`'te bir toplar,
aradaki tick'lerde önbellekteki snapshot'ı döner (aynı bytes → backend dedup
eder, pencere başına tek satır saklar). WAF/nginx sayıları docker socket'ten
container log'undan; fail2ban sayıları auto-ban log dosyasından; firewall
drop'ları `CAP_NET_ADMIN` ister (olmadan kendini atlar).

| Arg            | Tip    | Varsayılan                                   | Açıklama |
|----------------|--------|----------------------------------------------|----------|
| window_seconds | number | `3600`                                       | Toplama penceresi + cadence |
| socket         | string | `/var/run/docker.sock`                       | Unix socket yolu |
| waf_container  | string | `icosys-waf`                                 | ModSecurity container adı |
| nginx_container| string | `icosys-nginx`                               | nginx container adı |
| banned_ips_log | string | `/var/lib/icosys/auto-ban/banned-ips.log`    | auto-ban log dosyası |
| firewall_chain | string | `DOCKER-USER`                                | DROP paketleri toplanacak iptables zinciri |

Şekil (yok olan kaynaklar atlanır):

```
{
  collected_at, window_seconds,
  waf:      { blocked, by_class: { sqli, rce, lfi, ... } },
  nginx:    { http_4xx, http_403, http_429 },
  fail2ban: { banned_total, banned_window },
  firewall: { dropped_packets }
}
```

## Yeni bir dosya ekleme

```yaml
# /etc/iclic-host-agent/collectors.d/10-wildfly.yaml
- id: wildfly_state
  primitive: exec
  args:
    cmd: [/opt/wildfly/bin/jboss-cli.sh, -c, /:read-attribute(name=server-state)]
    timeout_sec: 5
    parse: trimmed
  output_key: wildfly_server_state

- id: wildfly_running
  primitive: systemctl.is_active
  args: { unit: wildfly.service }
  output_key: wildfly_running

- id: wildfly_admin_port
  primitive: tcp.connect
  args: { host: 127.0.0.1, port: 9990, timeout_sec: 2 }
  output_key: wildfly_admin_port_open
```

Dosyayı kaydet → bir tick bekle (varsayılan 60 sn) → ICLIC sunucu detay sayfası
yeni key'leri otomatik alır. Server Detail "Host Metrics" paneli varsayılan olarak
linux-host profilinin iyi bilinen key'lerini render eder; geri kalan her şey
"Heartbeat History" panelindeki ham payload görüntüleyicisinden görülebilir.

## Runtime deployment durumu

ICLIC, rezerve `runtime_instances` output key'i altında runtime versiyon
sinyallerini de kabul eder. Ajan, normal host heartbeat'i başarılı olduktan sonra
her öğeyi `POST /api/v1/server/runtime-instances/heartbeat`'e iletir. Bu, Docker,
systemd, WildFly, PHP, Python, .NET, Go ve diğer eski stack'leri tek bir toplama
yolunda tutar.

En kolay entegrasyon noktası, JSON yazan bir operatör script'i ve `parse: json`
olan bir `exec` binding'idir:

```yaml
# /etc/iclic-host-agent/collectors.d/40-runtime-instances.yaml
- id: runtime_instances
  primitive: exec
  args:
    cmd: [/opt/iclic-host-agent/runtime-discovery.sh]
    timeout_sec: 5
    parse: json
  output_key: runtime_instances
```

Script bir dizi yazdırmalı:

```json
[
  {
    "productCode": "ICOSYS",
    "componentCode": "hrm-backend",
    "instanceKey": "prod-api-01:hrm-backend",
    "runningVersion": "1.21.1",
    "gitCommit": "abc1234",
    "environment": "PROD",
    "payload": { "source": "systemd", "unit": "icosys-hrm.service" }
  }
]
```

`productCode` + `componentCode`, ICLIC runtime component katalog satırını
tanımlar. `instanceKey` restart'lar arası kararlı olmalı. Verilmezse ICLIC,
kimlik doğrulamalı server id + component code'a geri düşer. `versionSource` ve
`status` opsiyoneldir; ajan bunları `HOST_AGENT` ve `HEALTHY` olarak varsayar.
Endpoint sözleşmesi için [`protokol.md`](protokol.md).

## Güvenlik notları

- `/etc/iclic-host-agent/collectors.d/` izinleri `0750 root:iclic-agent`. Yalnızca
  root yazabilir — ajan yalnızca okur.
- Ajan `iclic-agent` sistem kullanıcısı olarak çalışır. `exec` primitive'i bu
  kullanıcının ayrıcalıklarını miras alır. Bir probe'un ayrıcalıklı bir dosyayı
  (örn. `/var/log/audit/audit.log`) okuması gerektiğinde operatörler ajanı root
  çalıştırmak yerine genelde grubu ACL ile yetkilendirir.
- Probe'lar binding başına timeout (varsayılan 5 sn) ve tick başına toplam bütçe
  (30 sn) ile çalışır. Patolojik bir probe heartbeat'i asla bloklamaz.
