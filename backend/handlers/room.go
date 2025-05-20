package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
)

var roomClients = make(map[int]map[*websocket.Conn]bool)
var broadcast = make(chan ChatMessage)

type ChatMessage struct {
	ID         int      `json:"id"`
	Text       string   `json:"text"`
	Sender     string   `json:"sender"`
	ReadCount  int      `json:"read_count,omitempty"`
	ReadStatus string   `json:"read_status,omitempty"`
	Images     []string `json:"images,omitempty"`
	RoomID     int      `json:"room_id,omitempty"`
	Type       string   `json:"type,omitempty"`
	UserID     int      `json:"userId,omitempty"`
	ClientID   string   `json:"client_id,omitempty"`
}

// WebSocket接続処理
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	roomIDStr := r.URL.Query().Get("roomId")
	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		http.Error(w, "roomIdが不正です", http.StatusBadRequest)
		return
	}

	token := r.Header.Get("Sec-WebSocket-Protocol")
	log.Printf("📥 Sec-WebSocket-Protocol（受信）: %s", token)

	if token == "" {
		log.Println("❌ トークンが空です")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	localUpgrader := upgrader
	localUpgrader.Subprotocols = []string{token}
	responseHeader := http.Header{}
	responseHeader.Set("Sec-WebSocket-Protocol", token)

	conn, err := localUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Println("❌ WebSocketアップグレード失敗:", err)
		return
	}

	log.Printf("🔁 conn.Subprotocol(): %s", conn.Subprotocol())

	// ✅ validateJWTFromTokenString を使用
	claims, err := validateJWTFromTokenString(token)
	if err != nil {
		log.Println("❌ JWT検証失敗:", err)
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Unauthorized"))
		conn.Close()
		return
	}

	username := claims.Username
	userID := claims.UserID
	log.Printf("✅ JWT検証成功: ユーザー名=%s", username)

	if roomClients[roomID] == nil {
		roomClients[roomID] = make(map[*websocket.Conn]bool)
	}
	roomClients[roomID][conn] = true
	log.Printf("📡 WebSocket接続: roomId=%d, user=%s", roomID, username)

	defer func() {
		delete(roomClients[roomID], conn)
		conn.Close()
		log.Println("🔌 WebSocket切断:", username)
	}()

	for {
		var msg ChatMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("🔌 通常切断: %v", err)
			} else {
				log.Printf("❌ WebSocket読み込みエラー: %v", err)
			}
			break
		}

		msg.Sender = username
		msg.UserID = userID
		msg.RoomID = roomID

		log.Printf("📨 受信: %+v", msg)

		switch msg.Type {
		case "read":
			log.Printf("📨 既読通知受信: userID=%d, sender=%s, clientID=%s, roomId=%d", msg.UserID, msg.Sender, msg.ClientID, msg.RoomID)
			// ✅ roomPresenceMap ログの追加（parsed不要）
			roomID := msg.RoomID
			presence := false
			if m, exists := roomPresenceMap[roomID]; exists {
				presence = m[userID]
			}
			log.Printf("🧪 roomPresenceMap確認: roomId=%d, userId=%d, presence=%v", roomID, userID, presence)

			go func(userID, roomID int) {
				_, err := DB.Exec(`
					INSERT INTO message_reads (user_id, message_id)
					SELECT $1, m.id
					FROM messages m
					WHERE m.room_id = $2
					AND m.sender_id != $1
					AND NOT EXISTS (
						SELECT 1 FROM message_reads mr WHERE mr.user_id = $1 AND mr.message_id = m.id
					)
				`, userID, roomID)
				if err != nil {
					log.Printf("❌ message_reads 挿入失敗: %v", err)
				}
			}(msg.UserID, msg.RoomID)

			msg.Text = ""
			msg.Images = nil
			broadcast <- msg
		case "ping":
			log.Println("📡 ping受信（切断防止）")
		case "message", "":
			broadcast <- msg
		default:
			log.Printf("⚠️ 未知のType: %s", msg.Type)
		}
	}
}

// メッセージをブロードキャスト
func StartBroadcaster() {
	for msg := range broadcast {
		if clients, ok := roomClients[msg.RoomID]; ok {
			for conn := range clients {
				if err := conn.WriteJSON(msg); err != nil {
					log.Println("❌ WebSocket送信失敗:", err)
					conn.Close()
					delete(clients, conn)
				}
			}
		}
	}
}

// 任意の関数から送信するブロードキャスト関数
func BroadcastMessage(roomID int, msg ChatMessage) {
	msg.RoomID = roomID
	msg.Type = "message"
	broadcast <- msg
}

type Room struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	MemberCnt   int    `json:"member_count"`
	IsGroup     int    `json:"is_group"`
	UnreadCount int    `json:"unread_count"`
}

func GetOwnedRooms(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("userId")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		writeJSONError(w, "不正なユーザーID", http.StatusBadRequest)
		return
	}

	rows, err := DB.Query(`
		SELECT r.id, 
		       COALESCE(NULLIF(r.room_name, ''), (
		         SELECT u.username 
		         FROM room_members rm
		         JOIN users u ON u.id = rm.user_id
		         WHERE rm.room_id = r.id AND rm.user_id != $1
		         LIMIT 1
		       )) AS room_name,
		       r.is_group,
		       (SELECT COUNT(*) FROM room_members WHERE room_id = r.id) AS member_count,
		       (
		         SELECT COUNT(*) FROM messages m
		         WHERE m.room_id = r.id
		         AND m.sender_id != $1
		         AND NOT EXISTS (
		           SELECT 1 FROM message_reads mr WHERE mr.message_id = m.id AND mr.user_id = $1
		         )
		       ) AS unread_count
		FROM chat_rooms r
		WHERE r.id IN (
			SELECT room_id FROM room_members WHERE user_id = $1
		)
		ORDER BY r.is_group DESC, r.id
	`, userID)
	if err != nil {
		writeJSONError(w, "DB取得エラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.IsGroup, &room.MemberCnt, &room.UnreadCount); err == nil {
			rooms = append(rooms, room)
		}
	}
	json.NewEncoder(w).Encode(rooms)
}

func CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   int    `json:"userId"`
		RoomName string `json:"roomName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	var exists int
	err := DB.QueryRow("SELECT COUNT(*) FROM chat_rooms WHERE room_name = $1", req.RoomName).Scan(&exists)
	if err != nil {
		writeJSONError(w, "DB検索エラー", http.StatusInternalServerError)
		return
	}
	if exists > 0 {
		writeJSONError(w, "この名前は既に利用されています", http.StatusBadRequest)
		return
	}

	var roomID int
	err = DB.QueryRow(`
		INSERT INTO chat_rooms (room_name, is_group, created_at, updated_at)
		VALUES ($1, 1, now(), now()) RETURNING id
	`, req.RoomName).Scan(&roomID)
	if err != nil {
		writeJSONError(w, "ルーム作成エラー", http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec("INSERT INTO room_members (room_id, user_id) VALUES ($1, $2)", roomID, req.UserID)
	if err != nil {
		writeJSONError(w, "メンバー登録エラー", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"roomId":  roomID,
		"message": "作成成功",
	})
}

func JoinRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int `json:"userId"`
		RoomID int `json:"roomId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	var exists int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM room_members WHERE user_id = $1 AND room_id = $2
	`, req.UserID, req.RoomID).Scan(&exists)
	if err != nil {
		writeJSONError(w, "DBチェックエラー", http.StatusInternalServerError)
		return
	}
	if exists > 0 {
		writeJSONError(w, "既に参加しています", http.StatusBadRequest)
		return
	}

	_, err = DB.Exec("INSERT INTO room_members (room_id, user_id) VALUES ($1, $2)", req.RoomID, req.UserID)
	if err != nil {
		writeJSONError(w, "ルーム参加エラー", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "参加成功"})
}

func LeaveRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, "不正なメソッド", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID int `json:"roomId"`
		UserID int `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	_, err := DB.Exec(`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, req.RoomID, req.UserID)
	if err != nil {
		writeJSONError(w, "脱退処理失敗", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "脱退成功"})
}

func GetRoomName(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	roomIDStr := r.URL.Query().Get("roomId")
	userIDStr := r.URL.Query().Get("userId")
	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		writeJSONError(w, "不正なルームID", http.StatusBadRequest)
		return
	}
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		writeJSONError(w, "不正なユーザーID", http.StatusBadRequest)
		return
	}

	var isGroup int
	err = DB.QueryRow("SELECT is_group FROM chat_rooms WHERE id = $1", roomID).Scan(&isGroup)
	if err != nil {
		writeJSONError(w, "ルーム種別取得失敗", http.StatusInternalServerError)
		return
	}

	var roomName string
	if isGroup == 1 {
		err = DB.QueryRow("SELECT room_name FROM chat_rooms WHERE id = $1", roomID).Scan(&roomName)
	} else {
		err = DB.QueryRow(`
			SELECT u.username
			FROM room_members rm
			JOIN users u ON rm.user_id = u.id
			WHERE rm.room_id = $1 AND rm.user_id != $2
		`, roomID, userID).Scan(&roomName)
	}
	if err != nil {
		writeJSONError(w, "ルーム名取得失敗", http.StatusInternalServerError)
		return
	}

	var memberCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM room_members WHERE room_id = $1", roomID).Scan(&memberCount)
	if err != nil {
		writeJSONError(w, "参加人数取得失敗", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"roomName":    roomName,
		"memberCount": memberCount,
		"isGroup":     isGroup == 1,
	})
}

func CreateOrGetOneToOneRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User1ID int `json:"user1Id"`
		User2ID int `json:"user2Id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	u1, u2 := req.User1ID, req.User2ID
	if u1 > u2 {
		u1, u2 = u2, u1
	}

	var roomID int
	err := DB.QueryRow(`
		SELECT r.id
		FROM chat_rooms r
		WHERE r.is_group = 0
		AND EXISTS (SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.user_id = $1)
		AND EXISTS (SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.user_id = $2)
		AND (SELECT COUNT(*) FROM room_members m WHERE m.room_id = r.id) = 2
		LIMIT 1
	`, u1, u2).Scan(&roomID)

	if err == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"roomId":  roomID,
			"message": "既存ルームに移動します",
		})
		return
	}

	err = DB.QueryRow(`
		INSERT INTO chat_rooms (room_name, is_group, created_at, updated_at)
		VALUES ('', 0, now(), now()) RETURNING id
	`).Scan(&roomID)
	if err != nil {
		writeJSONError(w, "ルーム作成失敗", http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec(`
		INSERT INTO room_members (room_id, user_id)
		VALUES ($1, $2), ($1, $3)
	`, roomID, u1, u2)
	if err != nil {
		writeJSONError(w, "メンバー登録失敗", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"roomId":  roomID,
		"message": "新規ルームを作成しました",
	})
}

func GetAvailableRooms(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("userId")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		writeJSONError(w, "不正なユーザーID", http.StatusBadRequest)
		return
	}

	rows, err := DB.Query(`
		SELECT r.id, r.room_name, r.is_group,
			(SELECT COUNT(*) FROM room_members WHERE room_id = r.id) AS member_count,
			0 AS unread_count
		FROM chat_rooms r
		WHERE r.is_group = 1
		AND r.id NOT IN (
			SELECT room_id FROM room_members WHERE user_id = $1
		)
	`, userID)
	if err != nil {
		writeJSONError(w, "DB取得エラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.IsGroup, &room.MemberCnt, &room.UnreadCount); err == nil {
			rooms = append(rooms, room)
		}
	}

	json.NewEncoder(w).Encode(rooms)
}
