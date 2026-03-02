# MyBot — Tài liệu đầy đủ

> Phiên bản: 2.0 • Cập nhật: 2026-03-02
> Ngôn ngữ: Go 1.25+ • Giao thức: Facebook Messenger (Messagix / mautrix-meta)

---

## Mục lục

1. [Tổng quan kiến trúc](#1-tổng-quan-kiến-trúc)
2. [Cài đặt & Chạy bot](#2-cài-đặt--chạy-bot)
3. [Cấu hình (`config.json`)](#3-cấu-hình-configjson)
4. [Cấu trúc lệnh](#4-cấu-trúc-lệnh)
5. [Danh sách lệnh](#5-danh-sách-lệnh)
6. [Tự động phát hiện media (Auto-detect)](#6-tự-động-phát-hiện-media-auto-detect)
7. [Messaging API — Gửi / Reply / Edit / Recall](#7-messaging-api--gửi--reply--edit--recall)
8. [Conversation API — Đọc lịch sử & Truy vấn](#8-conversation-api--đọc-lịch-sử--truy-vấn)
9. [Hệ thống Cooldown](#9-hệ-thống-cooldown)
10. [Cơ chế kết nối & Tự động reconnect](#10-cơ-chế-kết-nối--tự-động-reconnect)
11. [Lưu trữ dữ liệu (SQLite)](#11-lưu-trữ-dữ-liệu-sqlite)
12. [Nền tảng media được hỗ trợ](#12-nền-tảng-media-được-hỗ-trợ)
13. [Phát triển module mới](#13-phát-triển-module-mới)
14. [Build & Deploy](#14-build--deploy)
15. [Cấu trúc thư mục dự án](#15-cấu-trúc-thư-mục-dự-án)
16. [Gỡ lỗi & Logs](#16-gỡ-lỗi--logs)
17. [Giới hạn & Lưu ý kỹ thuật](#17-giới-hạn--lưu-ý-kỹ-thuật)

---

## 1. Tổng quan kiến trúc

```
┌─────────────────────────────────────────────────────────┐
│  Facebook Messenger (Messagix WebSocket)                │
└────────────────────────┬────────────────────────────────┘
                         │ events (LSTable)
                         ▼
┌─────────────────────────────────────────────────────────┐
│  cmd/bot/main.go — Event Loop                           │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────┐ │
│  │ handleEvent  │→ │ handleMessage│→ │ Registry.Exec  │ │
│  └─────────────┘  └──────┬───────┘  └───────┬────────┘ │
│                    auto-detect URL    command dispatch   │
└────────────────────────┬─────────────────────┬──────────┘
                         │                     │
          ┌──────────────▼───┐    ┌────────────▼────────┐
          │  media/*         │    │  modules/*           │
          │  (download/parse)│    │  (ping,help,roll,..) │
          └──────────────────┘    └────────────┬────────┘
                                               │
                              ┌────────────────▼────────────┐
                              │  messaging.Service           │
                              │  (SendText, Reply, Edit,     │
                              │   Recall, GetMessage, ...)   │
                              └────────────────┬────────────┘
                                               │
                    ┌──────────────────────────┼────────────────┐
                    │                          │                │
         ┌──────────▼──────────┐  ┌────────────▼─────────┐     │
         │  transport/facebook │  │  SQLiteStore          │     │
         │  (Messagix client)  │  │  (lưu trữ messages,  │     │
         │                     │  │   threads, users)     │     │
         └─────────────────────┘  └──────────────────────┘     │
                                  ┌──────────────────────┐     │
                                  │  Projector            │◄────┘
                                  │  (LSTable → DB sync)  │
                                  └──────────────────────┘
```

**Luồng xử lý tin nhắn:**
1. WebSocket nhận event từ Facebook → `handleEvent()`
2. Projector cập nhật threads/users/messages vào SQLite
3. Với mỗi tin nhắn mới → filter (bỏ tự gửi, tin cũ, trùng lặp)
4. Nếu bắt đầu bằng `!` → dispatch lệnh qua Registry
5. Nếu chứa URL (không phải lệnh) → auto-detect và tải media
6. Kết quả được gửi lại qua `messaging.Service` → Facebook transport

---

## 2. Cài đặt & Chạy bot

### Yêu cầu
- Go 1.25.5 trở lên
- Tài khoản Facebook có cookie hợp lệ
- (Tuỳ chọn) UPX để nén binary

### Chạy nhanh

```bash
# Clone repo
cd mybot

# Tạo config từ template
cp config.example.json config.json
# → Chỉnh sửa config.json (xem mục 3)

# Chạy trực tiếp
go run ./cmd/bot

# Hoặc build rồi chạy
go build -o bot ./cmd/bot
./bot
```

### Biến môi trường

| Biến | Mô tả | Mặc định |
|------|--------|----------|
| `LOG_FORMAT` | `json` → JSON logs, khác → console đẹp | console |

### Tín hiệu dừng

Bot lắng nghe `SIGINT` (Ctrl+C) và `SIGTERM` để tắt gracefully.

---

## 3. Cấu hình (`config.json`)

```jsonc
{
  // Prefix cho lệnh (mặc định "!")
  "command_prefix": "!",

  // Cách 1: Dán chuỗi cookie thô (tự động parse)
  // Format: "c_user=...;xs=...;fr=...;datr=...|ACCESS_TOKEN"
  "cookie_string": "",

  // Cách 2: Khai báo từng cookie riêng
  "cookies": {
    "c_user": "",
    "xs": "",
    "fr": "",
    "datr": ""
  },

  // Đường dẫn lưu SQLite DB (tương đối so với config.json)
  "storage": {
    "message_db_path": "data/messages.sqlite"
  },

  // Chu kỳ reconnect tự động (giây). 0 = tắt.
  "force_refresh_interval_seconds": 3600,

  // Bật/tắt từng module
  "modules": {
    "ping": true,
    "media": true,
    "help": true,
    "uptime": true,
    "info": true,
    "say": true,
    "coinflip": true,
    "roll": true
  }
}
```

### Chi tiết cấu hình

| Trường | Kiểu | Mô tả |
|--------|------|-------|
| `command_prefix` | `string` | Ký tự mở đầu lệnh. VD: `"!"` → `!ping` |
| `cookie_string` | `string` | Chuỗi cookie thô, phần sau `\|` là access token |
| `cookies` | `map` | Cookie key-value. Nếu cả 2 đều có, `cookie_string` ghi đè |
| `storage.message_db_path` | `string` | Đường dẫn SQLite. Tương đối → dựa trên vị trí config.json |
| `force_refresh_interval_seconds` | `int` | Reconnect định kỳ. Mặc định 3600 (1 giờ). Đặt `0` để tắt |
| `modules` | `map` | `true`/`false` cho từng module. Nếu map rỗng → tất cả bật |

### Cách lấy cookie Facebook

1. Đăng nhập Facebook trên trình duyệt
2. Mở DevTools (F12) → Application → Cookies → `https://www.facebook.com`
3. Copy giá trị của: `c_user`, `xs`, `fr`, `datr`
4. Dán vào `cookies` hoặc ghép thành `cookie_string`:
   ```
   c_user=123456;xs=abc:def;fr=xyz;datr=abc123
   ```

---

## 4. Cấu trúc lệnh

### Cú pháp chung

```
<prefix><tên_lệnh> [tham_số_1] [tham_số_2] ...
```

**Ví dụ** (với prefix mặc định `!`):
```
!ping
!roll 100
!media https://www.instagram.com/p/ABC123/
!say Xin chào mọi người
```

### Quy tắc:
- **Prefix**: Mặc định `!`, có thể thay đổi trong config
- **Tên lệnh**: Không phân biệt hoa/thường (`!PING` = `!ping`)
- **Tham số**: Cách nhau bởi khoảng trắng, truyền qua `ctx.Args[]`
- **Cooldown**: Mỗi lệnh có 3 giây cooldown theo người dùng
- **Ưu tiên**: Lệnh luôn ưu tiên hơn auto-detect media (nếu tin nhắn bắt đầu bằng prefix)

### Khi lệnh thất bại

Bot gửi tin nhắn lỗi: `Lỗi: <chi tiết lỗi>`. Các trường hợp:
- Lệnh không tồn tại: `"Lỗi: không tìm thấy lệnh: xyz"`
- Đang cooldown: `"Lỗi: vui lòng chờ 2.5 giây"`
- Lỗi thực thi: nội dung lỗi cụ thể tuỳ module

---

## 5. Danh sách lệnh

### 🏓 `ping` — Module: `ping`

Kiểm tra bot còn sống không.

```
!ping
```
**Phản hồi:** `Pong!`

---

### 📋 `help` — Module: `help`

Hiển thị danh sách tất cả lệnh đã đăng ký.

```
!help
```
**Phản hồi:**
```
📋 Danh sách lệnh:
- about: Thông tin về bot
- coinflip: Tung đồng xu (Sấp/Ngửa)
- help: Hiển thị danh sách các lệnh
- id: Hiển thị thông tin ID
- media: Tải media từ Facebook, TikTok, Douyin, Instagram
- ping: Trả lời Pong!
- roll: Tung xúc xắc (mặc định 1-6, hoặc !roll <số>)
- say: Lặp lại tin nhắn của bạn
- status: Kiểm tra trạng thái hệ thống
- uptime: Hiển thị thời gian bot đã hoạt động
```

---

### 🎬 `media` — Module: `media`

Tải xuống và gửi media từ URL.

```
!media <URL>
```

**Ví dụ:**
```
!media https://www.instagram.com/reel/ABC123/
!media https://www.tiktok.com/@user/video/123456
!media https://www.facebook.com/share/v/1DXMCN1e1T/
!media https://v.douyin.com/iYAbc123/
```

**Luồng xử lý:**
1. Bot gửi `"Tìm thấy N media, đang xử lý..."` 
2. Tải xuống song song tất cả media items
3. Gửi tất cả media trong 1 tin nhắn duy nhất (giữ nguyên thứ tự)

**Lỗi có thể:**
- `"đường dẫn không hợp lệ"` — URL không bắt đầu bằng `http`
- `"Lỗi: unsupported platform"` — Nền tảng không được hỗ trợ 
- `"Tải xuống #N thất bại:"` — Một media item tải thất bại
- `"tất cả media đều thất bại"` — Không có media nào tải được

---

### ⏱ `uptime` — Module: `uptime`

Thời gian bot đã hoạt động từ lúc khởi động.

```
!uptime
```
**Phản hồi:** `⏱ Thời gian hoạt động: 2h15m30s`

---

### 🤖 `about` — Module: `info`

Thông tin cơ bản về bot.

```
!about
```
**Phản hồi:** `🤖 MyBot v2.0 - Bot Messenger mô-đun`

---

### 👤 `id` — Module: `info`

Hiển thị ID người gửi và cuộc trò chuyện hiện tại.

```
!id
```
**Phản hồi:**
```
👤 ID người dùng: 61581248120082
💬 ID cuộc trò chuyện: 100045678901234
```

---

### 📊 `status` — Module: `info`

Trạng thái hệ thống chi tiết.

```
!status
```
**Phản hồi:**
```
📊 Bot Status
⏱ Uptime: 2h 15m 30s
💾 RAM: 45.72 MB
📦 Alloc: 12.34 MB
🔄 GC Cycles: 156
🧵 Goroutines: 23
💻 OS/Arch: linux/amd64
🔧 Go: go1.25.5
```

---

### 🗣 `say` — Module: `say`

Bot lặp lại tin nhắn của bạn.

```
!say <nội dung bất kỳ>
```

**Ví dụ:**
```
!say Xin chào mọi người
```
**Phản hồi:** `🗣 Xin chào mọi người`

**Lỗi:** `"cách dùng: !say <tin nhắn>"` nếu không có nội dung.

---

### 🪙 `coinflip` — Module: `coinflip`

Tung đồng xu ngẫu nhiên.

```
!coinflip
```
**Phản hồi:** `🪙 Ngửa` hoặc `🪙 Sấp` (50/50)

---

### 🎲 `roll` — Module: `roll`

Tung xúc xắc ngẫu nhiên.

```
!roll          → tung 1-6 (mặc định)
!roll 100      → tung 1-100
!roll 20       → tung 1-20
```

**Phản hồi:** `🎲 Kết quả: 4 (1-6)`

**Quy tắc:**
- Không có tham số: tung 1–6
- Có số > 1: tung 1–số đó
- Số ≤ 1 hoặc không phải số: bỏ qua, dùng mặc định 6

---

## 6. Tự động phát hiện media (Auto-detect)

Khi module `media` được bật, bot **tự động** phát hiện URL trong tin nhắn bình thường (không phải lệnh) và tải media.

### Cách hoạt động

1. Tin nhắn **không** bắt đầu bằng command prefix
2. Bot tìm URL (regex: `https?://\S+`) trong nội dung
3. Kiểm tra URL thuộc nền tảng hỗ trợ (Instagram, TikTok, Douyin, Facebook)
4. Nếu khớp → tải & gửi media (im lặng, không thông báo tiến trình)
5. Nếu không tìm thấy media → không có phản hồi (im lặng bỏ qua)

### Ví dụ

Người dùng gửi:
```
Xem video này hay lắm https://www.tiktok.com/@user/video/123456
```
Bot tự động tải video TikTok và gửi vào nhóm chat.

### Lưu ý
- **Lệnh luôn ưu tiên hơn auto-detect:** Gửi `!say https://instagram.com/p/abc` sẽ thực thi lệnh `say`, KHÔNG tải media
- Auto-detect chỉ lấy URL đầu tiên tìm được
- Các ký tự `.,;:!?"'()[]{}><` ở cuối URL sẽ bị bỏ qua tự động

---

## 7. Messaging API — Gửi / Reply / Edit / Recall

Đây là các khả năng mà bot hỗ trợ thông qua `MessageController` interface. Các module có thể sử dụng đầy đủ các chức năng này qua `ctx.Messages`.

### 7.1 Gửi tin nhắn văn bản

```go
// Gửi tin nhắn đơn giản
ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Xin chào!")

// Hoặc qua Messages API (trả về bản ghi đã lưu)
rec, err := ctx.Messages.SendText(ctx.Ctx, core.SendTextRequest{
    ThreadID: ctx.ThreadID,
    Text:     "Xin chào!",
})
// rec.MessageID  → ID tin nhắn đã gửi
// rec.TimestampMs → thời gian gửi
```

### 7.2 Reply (Trả lời) tin nhắn

```go
// Reply trực tiếp bằng message ID
rec, err := ctx.Messages.ReplyText(
    ctx.Ctx,
    ctx.ThreadID,
    ctx.IncomingMessageID,  // ID tin nhắn muốn reply
    "Đây là câu trả lời!",
)

// Hoặc reply qua SendText với ReplyTo
rec, err := ctx.Messages.SendText(ctx.Ctx, core.SendTextRequest{
    ThreadID: ctx.ThreadID,
    Text:     "Đây là câu trả lời!",
    ReplyTo:  &core.ReplyTarget{MessageID: ctx.IncomingMessageID},
})
```

### 7.3 Gửi media (ảnh, video, file)

```go
// Gửi 1 file
ctx.Sender.SendMedia(ctx.Ctx, ctx.ThreadID, imageData, "photo.jpg", "image/jpeg")

// Gửi nhiều file trong 1 tin nhắn
ctx.Sender.SendMultiMedia(ctx.Ctx, ctx.ThreadID, []core.MediaAttachment{
    {Data: imgData1, Filename: "photo1.jpg", MimeType: "image/jpeg"},
    {Data: imgData2, Filename: "photo2.jpg", MimeType: "image/jpeg"},
    {Data: videoData, Filename: "video.mp4", MimeType: "video/mp4"},
})

// Qua Messages API (trả về bản ghi đã lưu)
rec, err := ctx.Messages.SendMedia(ctx.Ctx, core.SendMediaRequest{
    ThreadID: ctx.ThreadID,
    Items: []core.MediaAttachment{
        {Data: data, Filename: "file.jpg", MimeType: "image/jpeg"},
    },
    ReplyTo: &core.ReplyTarget{MessageID: "mid.xxx"}, // tuỳ chọn
})
```

**Giới hạn upload:** 25 MB mỗi file (do Facebook).

### 7.4 Edit (Chỉnh sửa) tin nhắn đã gửi

```go
// Sửa tin nhắn theo message ID
rec, err := ctx.Messages.EditText(ctx.Ctx, messageID, "Nội dung đã sửa")
```

**Quy tắc:**
- Chỉ sửa được tin nhắn do bot gửi
- Cần có message ID (lấy từ `rec.MessageID` khi gửi, hoặc `GetLastBotMessage`)
- Xác nhận sửa qua WebSocket event (timeout 5 giây)

### 7.5 Recall (Thu hồi / Xoá) tin nhắn

```go
// Thu hồi tin nhắn
err := ctx.Messages.Recall(ctx.Ctx, messageID)
```

**Quy tắc:**
- Đánh dấu tin nhắn là `IsRecalled = true` trong DB
- Xoá khỏi last bot message tracking
- Facebook có thể giới hạn thời gian thu hồi

### 7.6 Lấy tin nhắn cuối cùng của bot

```go
// Lấy tin nhắn bot gửi gần nhất trong thread
rec, err := ctx.Messages.GetLastBotMessage(ctx.Ctx, ctx.ThreadID)
if rec != nil {
    // rec.MessageID, rec.Text, rec.TimestampMs, ...
}
```

Tự động bỏ qua tin nhắn đã bị recall.

### 7.7 Lấy tin nhắn theo ID

```go
rec, err := ctx.Messages.GetMessage(ctx.Ctx, "mid.xxxx")
```

---

## 8. Conversation API — Đọc lịch sử & Truy vấn

Thông qua `ctx.Conversation` (interface `ConversationReader`).

### 8.1 Lấy thông tin thread

```go
thread, err := ctx.Conversation.GetThread(ctx.Ctx, ctx.ThreadID)
// thread.ThreadID        → ID cuộc trò chuyện
// thread.Name            → Tên nhóm chat
// thread.LastActivityMs  → Timestamp hoạt động cuối
// thread.Deleted         → Đã bị xoá?
```

### 8.2 Lấy thông tin người dùng

```go
user, err := ctx.Conversation.GetUser(ctx.Ctx, ctx.SenderID)
// user.UserID            → ID Facebook
// user.Name              → Tên hiển thị
// user.UpdatedAtUnixMs   → Lần cập nhật cuối
```

### 8.3 Lấy lịch sử tin nhắn

```go
// Lấy 20 tin nhắn mới nhất trong thread
messages, err := ctx.Conversation.ListThreadMessages(ctx.Ctx, ctx.ThreadID, 20, "")

// Lấy 20 tin nhắn trước tin nhắn "mid.xxx" (phân trang)
messages, err := ctx.Conversation.ListThreadMessages(ctx.Ctx, ctx.ThreadID, 20, "mid.xxx")

for _, msg := range messages {
    // msg.MessageID, msg.Text, msg.SenderID, msg.TimestampMs
    // msg.IsFromBot, msg.IsEdited, msg.IsRecalled
    // msg.Attachments (metadata), msg.ReplyToMessageID
}
```

**Thứ tự:** Mới nhất → cũ nhất (DESC theo timestamp).

---

## 9. Hệ thống Cooldown

- **Mặc định:** 3 giây mỗi lệnh, mỗi người dùng
- **Phạm vi:** Theo cặp (sender_id, command_name) — case insensitive
- **Khi cooldown:** Bot trả lỗi `"vui lòng chờ X.X giây"`
- **Chỉ áp dụng khi lệnh thành công** — nếu lệnh lỗi, không set cooldown
- **Dọn dẹp:** Mỗi 5 phút, cooldown hết hạn bị xoá tự động

---

## 10. Cơ chế kết nối & Tự động reconnect

### Luồng kết nối

```
main() → runBot() → runBotOnce()
  1. Parse cookies → tạo Messagix client
  2. LoadMessagesPage() → lấy user info + initial data
  3. ObserveTable() → seed metadata (threads, users) vào DB
  4. Connect() → mở WebSocket
  5. Chờ Event_Ready → botReady = true
```

### Tự động reconnect

| Trigger | Hành vi |
|---------|---------|
| Periodic timer | Mặc định mỗi 3600 giây. Cấu hình qua `force_refresh_interval_seconds` |
| Socket error ≥ 10 lần | Full reconnect |
| Permanent error | Chờ 30 giây → full reconnect |
| Hết phiên / token hết hạn | Full reconnect (LoadMessagesPage lại) |

### Full reconnect bao gồm:
1. Disconnect WebSocket hiện tại
2. Tạo client mới với cookie mới
3. LoadMessagesPage lại (làm mới token)
4. Connect lại WebSocket

### Bảo vệ tin nhắn

- **Bỏ tin cũ:** Tin nhắn có timestamp < thời điểm connect bị bỏ qua
- **Chống trùng lặp:** sync.Map `seenMessages` theo message ID
- **Semaphore:** Tối đa 100 tin nhắn xử lý đồng thời

---

## 11. Lưu trữ dữ liệu (SQLite)

### Vị trí mặc định
`data/messages.sqlite` (tương đối so với `config.json`)

### Schema

**Bảng `threads`:**
| Cột | Kiểu | Mô tả |
|-----|------|-------|
| `thread_id` | INTEGER PK | ID cuộc hội thoại |
| `name` | TEXT | Tên nhóm / người |
| `updated_at_ms` | INTEGER | Lần update cuối (epoch ms) |
| `last_activity_ms` | INTEGER | Hoạt động cuối (epoch ms) |
| `deleted` | INTEGER | 1 nếu đã xoá |

**Bảng `users`:**
| Cột | Kiểu | Mô tả |
|-----|------|-------|
| `user_id` | INTEGER PK | ID Facebook |
| `name` | TEXT | Tên hiển thị |
| `updated_at_ms` | INTEGER | Lần update cuối |
| `deleted` | INTEGER | 1 nếu đã xoá |

**Bảng `messages`:**
| Cột | Kiểu | Mô tả |
|-----|------|-------|
| `message_id` | TEXT PK | ID tin nhắn |
| `thread_id` | INTEGER | ID thread |
| `sender_id` | INTEGER | ID người gửi |
| `sender_name_snapshot` | TEXT | Tên người gửi lúc gửi |
| `text` | TEXT | Nội dung văn bản |
| `reply_to_message_id` | TEXT | ID tin nhắn được reply |
| `offline_threading_id` | TEXT | OTID (chống trùng) |
| `is_from_bot` | INTEGER | 1 nếu bot gửi |
| `has_media` | INTEGER | 1 nếu có attachment |
| `attachments_json` | TEXT | JSON array metadata file đính kèm |
| `timestamp_ms` | INTEGER | Timestamp Facebook |
| `edit_count` | INTEGER | Số lần sửa |
| `is_edited` | INTEGER | 1 nếu đã sửa |
| `is_recalled` | INTEGER | 1 nếu đã thu hồi |
| `created_at_ms` | INTEGER | Thời gian tạo record |
| `updated_at_ms` | INTEGER | Thời gian cập nhật record |
| `recalled_at_ms` | INTEGER | Thời gian thu hồi |

**Bảng `thread_last_bot`:**
| Cột | Kiểu | Mô tả |
|-----|------|-------|
| `thread_id` | INTEGER PK | ID thread |
| `message_id` | TEXT | ID tin nhắn bot gửi cuối |

**Index:** `idx_messages_thread_ts` trên `(thread_id, timestamp_ms, message_id)` — tối ưu truy vấn lịch sử.

### Projector (LSTable → DB)

Bot tự động đồng bộ dữ liệu từ Facebook events vào SQLite:
- **Threads**: insert/update/delete/rename từ `LSUpdateOrInsertThread`, `LSDeleteThenInsertThread`, `LSSyncUpdateThreadName`, `LSDeleteThread`
- **Users**: từ `LSVerifyContactRowExists`, `LSDeleteThenInsertContact`, `LSVerifyContactParticipantExist`
- **Messages**: từ `LSInsertMessage`, `LSUpsertMessage` (wrapped), `LSEditMessage`, `LSDeleteMessage`
- **Missing metadata**: Khi gặp thread/user chưa có trong DB, bot tự gọi Facebook API để lấy metadata bổ sung

---

## 12. Nền tảng media được hỗ trợ

| Nền tảng | Domain nhận diện | Loại | Cách trích xuất |
|----------|------------------|------|-----------------|
| **Instagram** | `instagram.com`, `instagr.am` | Ảnh, video, carousel | GraphQL API (`/api/graphql`) |
| **TikTok** | `tiktok.com` (bao gồm `vm.`, `vt.`) | Video, slideshow | TikTok Feed API |
| **Douyin** (抖音) | `douyin.com`, `iesdouyin.com` | Video | Proxy API |
| **Facebook** | `facebook.com`, `fb.watch` | Video (HD ưu tiên), ảnh bài đăng | HTML scraping + regex |

### Chi tiết từng nền tảng

**Instagram:**
- Hỗ trợ: `/p/`, `/reel/`, `/tv/`, `/reels/`, `/share/p/`, `/share/reel/`
- Carousel (nhiều ảnh/video): tải tất cả items
- Share link (`/share/...`) tự động resolve redirect
- Retry: 10 lần với exponential backoff

**TikTok:**
- Hỗ trợ: `/video/`, `/photo/`, `/note/`
- Short link (`vm.tiktok.com`, `vt.tiktok.com`) tự động resolve
- Slideshow → tải tất cả ảnh
- Video → tải URL play_addr

**Douyin:**
- Short link (`v.douyin.com`) tự động resolve
- Video only (qua proxy API `douyin.cuong.one`)

**Facebook:**
- Share link (`/share/v/`, `/share/p/`, `/share/r/`) tự động resolve
- Video: thử HD trước (`browser_native_hd_url`, `playable_url_quality_hd`), fallback SD
- Ảnh: fallback nếu không tìm thấy video (`og:image`, `"image":{"uri":"..."}`)
- Dedup: cùng 1 URL ảnh chỉ gửi 1 lần

---

## 13. Phát triển module mới

### Bước 1: Tạo thư mục module

```
internal/modules/yourmodule/
    command.go
```

### Bước 2: Implement `core.CommandHandler`

```go
package yourmodule

import (
    "mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string        { return "yourcommand" }
func (c *Command) Description() string { return "Mô tả ngắn gọn" }

func (c *Command) Execute(ctx *core.CommandContext) error {
    // ctx.Args       → tham số lệnh ([]string)
    // ctx.RawText    → toàn bộ tin nhắn gốc
    // ctx.ThreadID   → ID cuộc trò chuyện
    // ctx.SenderID   → ID người gửi
    // ctx.IncomingMessageID → ID tin nhắn trigger

    // Gửi tin bình thường
    return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Kết quả!")

    // Hoặc sử dụng Messaging API đầy đủ:
    // ctx.Messages.SendText(...)
    // ctx.Messages.ReplyText(...)
    // ctx.Messages.EditText(...)
    // ctx.Messages.Recall(...)
    // ctx.Messages.GetLastBotMessage(...)

    // Hoặc đọc lịch sử:
    // ctx.Conversation.GetThread(...)
    // ctx.Conversation.GetUser(...)
    // ctx.Conversation.ListThreadMessages(...)
}
```

### Bước 3: Đăng ký trong `main.go`

```go
import "mybot/internal/modules/yourmodule"

// Trong main():
if enabled(cfg.Modules, "yourmodule") {
    cmds.Register(&yourmodule.Command{})
}
```

### Bước 4: Bật trong config

```json
{
  "modules": {
    "yourmodule": true
  }
}
```

### CommandContext — Tham khảo đầy đủ

| Field | Kiểu | Mô tả |
|-------|------|-------|
| `Ctx` | `context.Context` | Context cho request |
| `Sender` | `MessageSender` | Gửi text, media (interface đơn giản) |
| `Messages` | `MessageController` | API đầy đủ: send, reply, edit, recall, get |
| `Conversation` | `ConversationReader` | Đọc thread, user, lịch sử tin nhắn |
| `ThreadID` | `int64` | ID cuộc trò chuyện |
| `SenderID` | `int64` | ID người gửi lệnh |
| `IncomingMessageID` | `string` | ID tin nhắn chứa lệnh (dùng để reply) |
| `Args` | `[]string` | Tham số sau tên lệnh |
| `RawText` | `string` | Toàn bộ nội dung tin nhắn gốc |
| `StartTime` | `time.Time` | Thời gian bot khởi động |

### MessageSender — Interface gửi đơn giản

| Method | Mô tả |
|--------|-------|
| `SendMessage(ctx, threadID, text)` | Gửi văn bản |
| `SendMedia(ctx, threadID, data, filename, mimeType)` | Gửi 1 file |
| `SendMultiMedia(ctx, threadID, items)` | Gửi nhiều file |
| `GetSelfID()` | Lấy ID bot |

### MessageController — Interface đầy đủ

| Method | Mô tả |
|--------|-------|
| `SendText(ctx, SendTextRequest)` | Gửi text (có thể reply) → trả `*MessageRecord` |
| `SendMedia(ctx, SendMediaRequest)` | Gửi media (có thể reply) → trả `*MessageRecord` |
| `ReplyText(ctx, threadID, replyToMsgID, text)` | Reply text → trả `*MessageRecord` |
| `EditText(ctx, messageID, newText)` | Sửa tin nhắn → trả `*MessageRecord` |
| `Recall(ctx, messageID)` | Thu hồi tin nhắn |
| `GetMessage(ctx, messageID)` | Lấy tin nhắn theo ID |
| `GetLastBotMessage(ctx, threadID)` | Lấy tin bot gửi cuối trong thread |

### ConversationReader — Interface đọc dữ liệu

| Method | Mô tả |
|--------|-------|
| `GetThread(ctx, threadID)` | Thông tin thread |
| `GetUser(ctx, userID)` | Thông tin người dùng |
| `ListThreadMessages(ctx, threadID, limit, beforeMsgID)` | Lịch sử tin nhắn (phân trang) |

---

## 14. Build & Deploy

### Build cho hệ điều hành hiện tại

```bash
go build -o bot ./cmd/bot
```

### Cross-compile (Linux + Windows)

**Makefile:**
```bash
make all           # build cả Linux + Windows
make build-linux   # chỉ Linux
make build-windows # chỉ Windows
```

**PowerShell:**
```powershell
.\build_all.ps1
```

Output: `build_output/mybot-linux`, `build_output/mybot-windows.exe`

### Deploy

1. Copy binary + `config.json` lên server
2. Tạo thư mục `data/` (sẽ chứa SQLite DB)
3. Chạy:
   ```bash
   ./mybot-linux
   ```
4. Dừng: gửi `SIGINT` (Ctrl+C) hoặc `SIGTERM`

### Run tests

```bash
go test ./... -count=1 -short
```

---

## 15. Cấu trúc thư mục dự án

```
mybot/
├── cmd/bot/
│   └── main.go              # Entry point, event loop, message routing
├── internal/
│   ├── config/
│   │   └── config.go        # Load/parse config, cookie parsing
│   ├── core/
│   │   ├── interfaces.go    # CommandHandler, MessageSender, CommandContext
│   │   └── messaging.go     # MessageRecord, MessageController, ConversationReader
│   ├── media/
│   │   ├── downloader.go    # HTTP client, GetMedia(), DownloadMedia()
│   │   ├── types.go         # MediaItem, MediaType (Image/Video)
│   │   ├── instagram.go     # Instagram GraphQL scraper
│   │   ├── tiktok.go        # TikTok Feed API
│   │   ├── douyin.go        # Douyin proxy API
│   │   └── facebook.go      # Facebook HTML scraper
│   ├── messaging/
│   │   ├── service.go       # Orchestrator: send, reply, edit, recall
│   │   ├── projector.go     # LSTable → DB sync (threads, users, messages)
│   │   ├── tracker.go       # Edit confirmation tracker (WaitForEdit)
│   │   ├── store.go         # Store interface
│   │   ├── sqlite_store.go  # SQLite implementation
│   │   ├── bolt_store.go    # BoltDB implementation (alternative)
│   │   ├── transport.go     # Transport interface
│   │   └── errors.go        # Error constants
│   ├── modules/
│   │   ├── ping/            # !ping → Pong!
│   │   ├── help/            # !help → danh sách lệnh
│   │   ├── media/           # !media <url> → tải & gửi media
│   │   ├── uptime/          # !uptime → thời gian hoạt động
│   │   ├── info/            # !about, !id, !status
│   │   ├── say/             # !say <text> → lặp lại
│   │   ├── coinflip/        # !coinflip → tung đồng xu
│   │   └── roll/            # !roll [max] → tung xúc xắc
│   ├── registry/
│   │   └── registry.go      # Command registry + cooldown management
│   └── transport/
│       └── facebook/
│           └── client.go     # Messagix wrapper, send/edit/recall/upload
├── config.example.json       # Template cấu hình
├── Makefile                  # Build targets (linux, windows)
├── build_all.ps1             # PowerShell cross-compile script
└── go.mod                    # Go module definition
```

---

## 16. Gỡ lỗi & Logs

### Định dạng log

- **Console (mặc định):** Có màu, timestamp `15:04:05`
- **JSON:** Đặt `LOG_FORMAT=json` để xuất structured logs

### Các sự kiện log quan trọng

| Message | Ý nghĩa |
|---------|---------|
| `Logged in` | Đăng nhập thành công, kèm bot ID |
| `Bot is ready to process messages` | WebSocket connected, bắt đầu nhận tin |
| `Bot reconnected` | Kết nối lại thành công |
| `Received table update` | Nhận dữ liệu mới (số upsert, insert) |
| `Processing command` | Đang xử lý lệnh (kèm tên) |
| `Auto-detected media` | Phát hiện URL media (kèm số items) |
| `Socket error` | Lỗi WebSocket (kèm số lần thử) |
| `Permanent connection error` | Lỗi không thể recover |
| `Periodic reconnect timer fired` | Reconnect định kỳ |
| `Full reconnect triggered` | Bắt đầu reconnect toàn phần |

---

## 17. Giới hạn & Lưu ý kỹ thuật

| Giới hạn | Giá trị | Ghi chú |
|----------|---------|---------|
| Kích thước upload tối đa | 25 MB / file | Do Facebook Messenger quy định |
| Kích thước download tối đa | 25 MB | Tự động reject nếu vượt quá |
| HTTP timeout | 30 giây | Cho media fetch |
| Concurrent messages | 100 | Semaphore giới hạn goroutine |
| Command cooldown | 3 giây / user / command | |
| Retry (external API) | 10 lần + backoff | Instagram, TikTok, Facebook media |
| Retry (Facebook send) | 3 lần + backoff | Send/upload tin nhắn |
| Dedup (seenMessages) | Xoá mỗi 5 phút | Có khoảng trống nhỏ khi rotate |
| Edit confirm timeout | 5 giây | Chờ xác nhận sửa từ WebSocket |
| Metadata refresh cooldown | 60 giây | Tránh spam LoadMessagesPage |
| SQLite connections | 1 (writer) | WAL mode, busy timeout 5s |
