package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func ChatHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		return
	}

	claims, err := validateJWT(r)
	if err != nil {
		writeJSONError(w, "認証失敗: "+err.Error(), http.StatusUnauthorized)
		return
	}
	username := claims.Username
	userID := claims.UserID

	if err := DB.QueryRow("SELECT id FROM users WHERE username = $1", username).Scan(&userID); err != nil {
		writeJSONError(w, "ユーザーID取得エラー", http.StatusInternalServerError)
		return
	}

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
		var input struct {
			Text     string   `json:"text"`
			ClientID string   `json:"client_id"`
			Images   []string `json:"images"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSONError(w, "JSON解析エラー", http.StatusBadRequest)
			return
		}

		var msgID int
		err = DB.QueryRow(`
			INSERT INTO messages (room_id, sender_id, content, client_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, roomID, userID, input.Text, input.ClientID, time.Now(), time.Now()).Scan(&msgID)
		if err != nil {
			writeJSONError(w, "メッセージ保存エラー", http.StatusInternalServerError)
			return
		}

		for _, image := range input.Images {
			_, _ = DB.Exec(`
				INSERT INTO message_attachments (message_id, file_name, created_at)
				VALUES ($1, $2, $3)
			`, msgID, image, time.Now())
		}

		msg := ChatMessage{
			ID:        msgID,
			Text:      input.Text,
			Sender:    username,
			ReadCount: 0,
			RoomID:    roomID,
			Type:      "message",
			UserID:    userID,
			ClientID:  input.ClientID,
			Images:    input.Images,
		}

		go BroadcastMessage(roomID, msg)
		json.NewEncoder(w).Encode(msg)

	case http.MethodGet:
		var isGroup int
		err := DB.QueryRow("SELECT is_group FROM chat_rooms WHERE id = $1", roomID).Scan(&isGroup)
		if err != nil {
			writeJSONError(w, "ルーム種別取得失敗", http.StatusInternalServerError)
			return
		}

		rows, err := DB.Query(`
			SELECT 
				m.id, 
				m.content, 
				u.username,
				(SELECT COUNT(*) FROM message_reads mr WHERE mr.message_id = m.id) AS read_count,
				m.client_id
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

		presenceMutex.Lock()
		roomUsers := roomPresenceMap[roomID]
		presenceMutex.Unlock()

		var messages []ChatMessage
		for rows.Next() {
			var msg ChatMessage
			if err := rows.Scan(&msg.ID, &msg.Text, &msg.Sender, &msg.ReadCount, &msg.ClientID); err != nil {
				continue
			}

			imgRows, _ := DB.Query(`SELECT file_name FROM message_attachments WHERE message_id = $1`, msg.ID)
			for imgRows.Next() {
				var fname string
				_ = imgRows.Scan(&fname)
				msg.Images = append(msg.Images, "http://localhost:8080/uploads/"+fname)
			}
			imgRows.Close()

			var alreadyReadUserIDs []int
			readRows, _ := DB.Query(`SELECT user_id FROM message_reads WHERE message_id = $1`, msg.ID)
			for readRows.Next() {
				var uid int
				_ = readRows.Scan(&uid)
				alreadyReadUserIDs = append(alreadyReadUserIDs, uid)
			}
			readRows.Close()

			newlyAdded := 0
			for uid, present := range roomUsers {
				if uid != userID && present && !contains(alreadyReadUserIDs, uid) {
					_, _ = DB.Exec(`INSERT INTO message_reads (message_id, user_id, read_at) VALUES ($1, $2, $3)`,
						msg.ID, uid, time.Now())
					newlyAdded++
				}
			}
			if newlyAdded > 0 {
				msg.ReadCount += newlyAdded
			}

			if msg.Sender == username && isGroup == 0 {
				isOtherUserInRoom := false
				for uid, present := range roomUsers {
					if uid != userID && present {
						isOtherUserInRoom = true
						break
					}
				}
				if isOtherUserInRoom {
					msg.ReadStatus = "既読"
				} else {
					msg.ReadStatus = "未読"
				}
			} else if msg.Sender != username && isGroup == 0 {
				msg.ReadStatus = "既読"
			}

			messages = append(messages, msg)
		}

		json.NewEncoder(w).Encode(messages)

	default:
		writeJSONError(w, "不正なメソッド", http.StatusMethodNotAllowed)
	}
}
