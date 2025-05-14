package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// チャットメッセージの構造体
type ChatMessage struct {
	ID         int      `json:"id"`                    // メッセージID
	Text       string   `json:"text"`                  // メッセージ本文
	Sender     string   `json:"sender"`                // 送信者のユーザー名
	ReadCount  int      `json:"read_count,omitempty"`  // グループルームでの既読数
	ReadStatus string   `json:"read_status,omitempty"` // 1対1ルームでの既読ステータス
	Images     []string `json:"images,omitempty"`      // 添付画像のURL配列
}

// メインのチャット処理ハンドラー（GET：メッセージ一覧取得、POST：メッセージ送信）
func ChatHandler(w http.ResponseWriter, r *http.Request) {
	// CORS 対応
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// プリフライトリクエスト処理（OPTIONS）
	if r.Method == http.MethodOptions {
		return
	}

	// JWTで認証
	username, err := validateJWT(r)
	if err != nil {
		writeJSONError(w, "認証失敗: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// ユーザーID取得
	var userID int
	if err := DB.QueryRow("SELECT id FROM users WHERE username = $1", username).Scan(&userID); err != nil {
		writeJSONError(w, "ユーザーID取得エラー", http.StatusInternalServerError)
		return
	}

	// ルームID取得
	roomIDStr := r.URL.Query().Get("roomId")
	if roomIDStr == "" {
		writeJSONError(w, "roomIdが必要です", http.StatusBadRequest)
		return
	}
	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		writeJSONError(w, "roomIdは整数である必要があります", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// 新規メッセージのPOST
		var input struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSONError(w, "JSON解析エラー", http.StatusBadRequest)
			return
		}

		// DBへメッセージ登録
		var msgID int
		err = DB.QueryRow(`
			INSERT INTO messages (room_id, sender_id, content, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`, roomID, userID, input.Text, time.Now(), time.Now()).Scan(&msgID)
		if err != nil {
			writeJSONError(w, "メッセージ保存エラー", http.StatusInternalServerError)
			return
		}

		// レスポンスとして新規メッセージを返す
		json.NewEncoder(w).Encode(ChatMessage{
			ID:        msgID,
			Text:      input.Text,
			Sender:    username,
			ReadCount: 0, // 投稿直後は既読なし
		})

	case http.MethodGet:
		// ルームがグループかどうか確認（既読の表示方法に影響）
		var isGroup int
		err := DB.QueryRow("SELECT is_group FROM chat_rooms WHERE id = $1", roomID).Scan(&isGroup)
		if err != nil {
			writeJSONError(w, "ルーム種別取得失敗", http.StatusInternalServerError)
			return
		}

		// メッセージと送信者情報取得
		rows, err := DB.Query(`
			SELECT m.id, m.content, u.username
			FROM messages m
			JOIN users u ON m.sender_id = u.id
			WHERE m.room_id = $1
			ORDER BY m.created_at ASC
		`, roomID)
		if err != nil {
			writeJSONError(w, "メッセージ取得失敗", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var messages []ChatMessage
		for rows.Next() {
			var msg ChatMessage
			if err := rows.Scan(&msg.ID, &msg.Text, &msg.Sender); err != nil {
				continue
			}

			// 既読カウント（自分以外）
			var readCount int
			_ = DB.QueryRow(`
				SELECT COUNT(*) FROM message_reads WHERE message_id = $1 AND user_id != $2
			`, msg.ID, userID).Scan(&readCount)

			// 既読情報登録（重複はスキップ）
			_, _ = DB.Exec(`
				INSERT INTO message_reads (message_id, user_id, read_at)
				VALUES ($1, $2, $3)
				ON CONFLICT (message_id, user_id) DO NOTHING
			`, msg.ID, userID, time.Now())

			// 添付画像を取得
			imgRows, _ := DB.Query(`
				SELECT file_name FROM message_attachments WHERE message_id = $1
			`, msg.ID)
			for imgRows.Next() {
				var fname string
				_ = imgRows.Scan(&fname)
				msg.Images = append(msg.Images, "http://localhost:8080/uploads/"+fname)
			}
			imgRows.Close()

			// 表示形式の切り替え（グループ: 既読数 / 1対1: 既読・未読）
			if isGroup == 1 {
				msg.ReadCount = readCount
			} else {
				if msg.Sender == username {
					if readCount > 0 {
						msg.ReadStatus = "既読"
					} else {
						msg.ReadStatus = "未読"
					}
				}
			}

			messages = append(messages, msg)
		}

		// すべてのメッセージを返却
		json.NewEncoder(w).Encode(messages)

	default:
		// GET/POST以外は許可しない
		writeJSONError(w, "不正なメソッド", http.StatusMethodNotAllowed)
	}
}
