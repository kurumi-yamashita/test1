package main

import (
	"log"
	"net/http"

	"backend/handlers"

	"github.com/rs/cors"
)

func main() {
	// ✅ DB初期化（戻り値がない関数）
	handlers.InitDB()
	defer handlers.DB.Close()

	// ✅ CORS設定（Next.jsフロントとの通信を許可）
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	// ✅ HTTPルーティング設定
	mux := http.NewServeMux()

	// 🔐 認証系エンドポイント
	mux.HandleFunc("/api/signup", handlers.SignupHandler)
	mux.HandleFunc("/api/login", handlers.LoginHandler)

	// 💬 チャットAPI（取得・投稿）
	mux.HandleFunc("/api/chat", handlers.ChatHandler)

	// 🏠 ルームAPI（グループ & 1対1）
	mux.HandleFunc("/api/rooms/owned", handlers.GetOwnedRooms)
	mux.HandleFunc("/api/rooms/available", handlers.GetAvailableRooms)
	mux.HandleFunc("/api/rooms/create", handlers.CreateRoom)
	mux.HandleFunc("/api/rooms/join", handlers.JoinRoom)
	mux.HandleFunc("/api/rooms/one-to-one", handlers.CreateOrGetOneToOneRoom)
	mux.HandleFunc("/api/rooms/name", handlers.GetRoomName) // ✅ ルーム名と人数の取得

	// 👤 ユーザー取得
	mux.HandleFunc("/api/users", handlers.GetAllUsers)

	// 📎 画像アップロードAPI
	mux.HandleFunc("/api/upload", handlers.UploadHandler)

	// 🖼 アップロード済み画像の配信
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// 🚀 サーバー起動
	log.Println("✅ Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", c.Handler(mux)))
}
