package main

import (
	"log"
	"net/http"
	"os"

	"backend/handlers"

	"github.com/rs/cors"
)

func main() {
	handlers.InitDB()
	defer handlers.DB.Close()

	go handlers.StartBroadcaster()

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	mux := http.NewServeMux()

	// 認証とユーザー
	mux.HandleFunc("/api/signup", handlers.SignupHandler)
	mux.HandleFunc("/api/login", handlers.LoginHandler)
	mux.HandleFunc("/api/users", handlers.GetAllUsers)

	// チャット関連
	mux.HandleFunc("/api/chat", handlers.ChatHandler)
	mux.HandleFunc("/api/upload", handlers.UploadHandler)

	// ルーム管理
	mux.HandleFunc("/api/rooms/owned", handlers.GetOwnedRooms)
	mux.HandleFunc("/api/rooms/available", handlers.GetAvailableRooms)
	mux.HandleFunc("/api/rooms/create", handlers.CreateRoom)
	mux.HandleFunc("/api/rooms/join", handlers.JoinRoom)
	mux.HandleFunc("/api/rooms/leave", handlers.LeaveRoom)
	mux.HandleFunc("/api/rooms/name", handlers.GetRoomName)
	mux.HandleFunc("/api/rooms/one-to-one", handlers.CreateOrGetOneToOneRoom)

	// 静的ファイル
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// WebSocket
	mux.HandleFunc("/ws", handlers.HandleWebSocket)
	mux.HandleFunc("/ws/notify", handlers.NotifyWebSocketHandler)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Println("✅ Server running on", port)
	log.Fatal(http.ListenAndServe(port, c.Handler(mux)))
}
