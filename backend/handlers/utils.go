package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Connection struct {
	Conn   *websocket.Conn
	UserID int
	RoomID int
}

var (
	roomConns   = make(map[int][]*Connection)
	globalConns = make([]*Connection, 0)
	connMutex   = &sync.Mutex{}
)

func AddRoomConnection(roomID int, conn *Connection) {
	connMutex.Lock()
	defer connMutex.Unlock()
	roomConns[roomID] = append(roomConns[roomID], conn)
}

func RemoveRoomConnection(roomID int, target *Connection) {
	connMutex.Lock()
	defer connMutex.Unlock()
	conns := roomConns[roomID]
	for i, c := range conns {
		if c == target {
			roomConns[roomID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
}

func BroadcastToRoom(roomID int, msg interface{}, exclude *Connection) {
	connMutex.Lock()
	conns := roomConns[roomID]
	connMutex.Unlock()
	for _, c := range conns {
		if c != exclude {
			if err := c.Conn.WriteJSON(msg); err != nil {
				log.Println("❌ WebSocket送信エラー:", err)
			}
		}
	}
}

func AddGlobalConnection(conn *Connection) {
	connMutex.Lock()
	defer connMutex.Unlock()
	globalConns = append(globalConns, conn)
}

func RemoveGlobalConnection(target *Connection) {
	connMutex.Lock()
	defer connMutex.Unlock()
	for i, c := range globalConns {
		if c == target {
			globalConns = append(globalConns[:i], globalConns[i+1:]...)
			break
		}
	}
}

func DisconnectExistingNotifyConnection(userID int) {
	connMutex.Lock()
	defer connMutex.Unlock()
	for i, c := range globalConns {
		if c.UserID == userID {
			log.Printf("⚠️ 既存Notify接続を切断: userID=%d", userID)
			_ = c.Conn.Close()
			globalConns = append(globalConns[:i], globalConns[i+1:]...)
			break
		}
	}
}

func BroadcastGlobal(msg interface{}) {
	connMutex.Lock()
	defer connMutex.Unlock()
	for _, c := range globalConns {
		if err := c.Conn.WriteJSON(msg); err != nil {
			log.Println("❌ グローバルWebSocket送信エラー:", err)
		}
	}
}

func WriteJSONError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

var roomPresenceMap = make(map[int]map[int]bool)
var presenceMutex = &sync.Mutex{}

func NotifyWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Sec-WebSocket-Protocol")
	if token == "" {
		log.Println("❌ トークンが空です")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	claims, err := validateJWTFromTokenString(token)
	if err != nil {
		log.Println("❌ JWT認証失敗:", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, http.Header{"Sec-WebSocket-Protocol": []string{token}})
	if err != nil {
		log.Println("❌ WebSocketアップグレード失敗:", err)
		return
	}

	userID := claims.UserID
	username := claims.Username
	c := &Connection{Conn: conn, UserID: userID}

	DisconnectExistingNotifyConnection(userID)
	AddGlobalConnection(c)
	log.Println("📡 Notify WebSocket接続:", username)

	defer func() {
		log.Println("🔌 Notify切断:", username)
		RemoveGlobalConnection(c)
		removeFromAllRooms(userID)
		log.Println("🧹 接続情報削除完了:", username)
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ 異常切断: %v", err)
			} else {
				log.Printf("🔌 通常切断: %v", err)
			}
			break
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(msg, &parsed); err != nil {
			log.Println("❌ Notify JSONパース失敗:", err)
			continue
		}

		log.Printf("🔔 通知受信: userID=%d, sender=%s, clientID=%v, roomId=%v, type=%v, action=%v\n",
			userID, username, parsed["client_id"], parsed["roomId"], parsed["type"], parsed["action"])

		if parsed["type"] == "presence" {
			roomIdAny, ok := parsed["roomId"]
			if !ok {
				log.Println("❌ roomIdが欠落")
				continue
			}
			roomIdFloat, ok := roomIdAny.(float64)
			if !ok {
				log.Println("❌ roomIdの形式がfloat64でない:", roomIdAny)
				continue
			}
			roomId := int(roomIdFloat)
			action, _ := parsed["action"].(string)
			updatePresence(roomId, userID, action)
		}

		BroadcastGlobal(parsed)
	}
}

func updatePresence(roomId int, userId int, action string) {
	presenceMutex.Lock()
	defer presenceMutex.Unlock()

	if _, exists := roomPresenceMap[roomId]; !exists {
		roomPresenceMap[roomId] = make(map[int]bool)
	}

	if action == "enter" {
		for _, members := range roomPresenceMap {
			delete(members, userId)
		}
		roomPresenceMap[roomId][userId] = true
		log.Printf("✅ [ENTER] userID=%d が roomID=%d に入室", userId, roomId)
	} else if action == "leave" {
		delete(roomPresenceMap[roomId], userId)
		log.Printf("❌ [LEAVE] userID=%d が roomID=%d から退室", userId, roomId)
	}

	log.Printf("📊 roomPresenceMap 状態: %+v", roomPresenceMap)
}

func removeFromAllRooms(userId int) {
	presenceMutex.Lock()
	defer presenceMutex.Unlock()
	for _, members := range roomPresenceMap {
		delete(members, userId)
	}
}
