package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// ルーム情報構造体（APIレスポンス用）
type Room struct {
	ID        int    `json:"id"`           // ルームID
	Name      string `json:"name"`         // ルーム名
	MemberCnt int    `json:"member_count"` // 参加人数
	IsGroup   int    `json:"is_group"`     // グループか否か（1:グループ, 0:1対1）
}

// ✅ 所属グループルーム取得（userId をもとに参加中のグループルームを返す）
func GetOwnedRooms(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("userId")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		writeJSONError(w, "不正なユーザーID", http.StatusBadRequest)
		return
	}
	log.Printf("🔍 ユーザーID %d のグループルーム取得", userID)

	// 所属しているグループルームを抽出するクエリ
	rows, err := DB.Query(`
		SELECT r.id, r.room_name, r.is_group,
			(SELECT COUNT(*) FROM room_members WHERE room_id = r.id) AS member_count
		FROM chat_rooms r
		WHERE r.is_group = 1 AND r.id IN (
			SELECT room_id FROM room_members WHERE user_id = $1
		)
	`, userID)
	if err != nil {
		writeJSONError(w, "DB取得エラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// 結果の整形
	rooms := make([]Room, 0)
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.IsGroup, &room.MemberCnt); err == nil {
			rooms = append(rooms, room)
		}
	}

	json.NewEncoder(w).Encode(rooms)
}

// ✅ 参加可能なグループルーム取得（未参加で is_group = 1 のルーム）
func GetAvailableRooms(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.URL.Query().Get("userId")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		writeJSONError(w, "不正なユーザーID", http.StatusBadRequest)
		return
	}
	log.Printf("🔍 ユーザーID %d の参加可能ルーム取得", userID)

	rows, err := DB.Query(`
		SELECT r.id, r.room_name, r.is_group,
			(SELECT COUNT(*) FROM room_members WHERE room_id = r.id) AS member_count
		FROM chat_rooms r
		WHERE r.id NOT IN (
			SELECT room_id FROM room_members WHERE user_id = $1
		) AND r.is_group = 1
	`, userID)
	if err != nil {
		writeJSONError(w, "DB取得エラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.IsGroup, &room.MemberCnt); err == nil {
			rooms = append(rooms, room)
		}
	}

	json.NewEncoder(w).Encode(rooms)
}

// ✅ グループルーム作成（同名チェック後、作成と作成者の登録）
func CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   int    `json:"userId"`
		RoomName string `json:"roomName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	// 同名ルームが存在するかチェック
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

	// ルーム作成
	var roomID int
	err = DB.QueryRow(`
		INSERT INTO chat_rooms (room_name, is_group, created_at, updated_at)
		VALUES ($1, 1, now(), now()) RETURNING id
	`, req.RoomName).Scan(&roomID)
	if err != nil {
		writeJSONError(w, "ルーム作成エラー", http.StatusInternalServerError)
		return
	}

	// 作成者をメンバーとして登録
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

// ✅ グループルームに参加（既参加チェック後に登録）
func JoinRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int `json:"userId"`
		RoomID int `json:"roomId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "無効なリクエスト", http.StatusBadRequest)
		return
	}

	// 既に参加しているか確認
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

	// 新たに登録
	_, err = DB.Exec("INSERT INTO room_members (room_id, user_id) VALUES ($1, $2)", req.RoomID, req.UserID)
	if err != nil {
		writeJSONError(w, "ルーム参加エラー", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "参加成功"})
}

// ✅ 1対1ルーム取得または作成（ユーザーIDを昇順に固定）
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
		u1, u2 = u2, u1 // 小さい順に並べることでペアを一意に固定
	}

	// 既存の1対1ルームを検索
	var roomID int
	err := DB.QueryRow(`
		SELECT r.id
		FROM chat_rooms r
		WHERE r.is_group = 0
		AND EXISTS (
			SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.user_id = $1
		)
		AND EXISTS (
			SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.user_id = $2
		)
		AND (
			SELECT COUNT(*) FROM room_members m WHERE m.room_id = r.id
		) = 2
		LIMIT 1
	`, u1, u2).Scan(&roomID)

	if err == nil {
		// 既存ルームが見つかった場合はそれを返す
		json.NewEncoder(w).Encode(map[string]interface{}{
			"roomId":  roomID,
			"message": "既存ルームに移動します",
		})
		return
	}

	// 新規ルーム作成
	err = DB.QueryRow(`
		INSERT INTO chat_rooms (room_name, is_group, created_at, updated_at)
		VALUES ('', 0, now(), now()) RETURNING id
	`).Scan(&roomID)
	if err != nil {
		writeJSONError(w, "ルーム作成失敗", http.StatusInternalServerError)
		return
	}

	// 両ユーザーを登録
	_, err = DB.Exec(`
		INSERT INTO room_members (room_id, user_id) VALUES ($1, $2), ($1, $3)
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

// ✅ ルーム名および参加人数取得（1対1ルームの場合は相手ユーザー名を返す）
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

	// グループルームか1対1ルームかを判定
	var isGroup int
	err = DB.QueryRow("SELECT is_group FROM chat_rooms WHERE id = $1", roomID).Scan(&isGroup)
	if err != nil {
		writeJSONError(w, "ルーム種別取得失敗", http.StatusInternalServerError)
		return
	}

	var roomName string
	if isGroup == 1 {
		// グループルーム名をそのまま返す
		err = DB.QueryRow("SELECT room_name FROM chat_rooms WHERE id = $1", roomID).Scan(&roomName)
		if err != nil {
			writeJSONError(w, "ルーム名取得失敗", http.StatusInternalServerError)
			return
		}
	} else {
		// 1対1ルームの場合、相手ユーザー名を返す
		err = DB.QueryRow(`
			SELECT u.username
			FROM room_members rm
			JOIN users u ON rm.user_id = u.id
			WHERE rm.room_id = $1 AND rm.user_id != $2
		`, roomID, userID).Scan(&roomName)
		if err != nil {
			writeJSONError(w, "相手ユーザー名取得失敗", http.StatusInternalServerError)
			return
		}
	}

	// 人数取得
	var memberCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM room_members WHERE room_id = $1", roomID).Scan(&memberCount)
	if err != nil {
		writeJSONError(w, "参加人数取得失敗", http.StatusInternalServerError)
		return
	}

	// 結果を返す
	json.NewEncoder(w).Encode(map[string]interface{}{
		"roomName":    roomName,
		"memberCount": memberCount,
		"isGroup":     isGroup == 1,
	})
}
