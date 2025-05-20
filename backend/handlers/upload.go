package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		log.Println("❌ Formパース失敗:", err)
		http.Error(w, "ファイルパース失敗: "+err.Error(), http.StatusBadRequest)
		return
	}

	messageIDStr := r.FormValue("message_id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		log.Println("❌ message_idエラー:", err)
		http.Error(w, "message_idが不正です: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("image")
	if err != nil {
		log.Println("❌ ファイル取得失敗:", err)
		http.Error(w, "ファイル取得失敗: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	log.Println("📎 アップロード開始: ファイル名 =", handler.Filename, ", message_id =", messageID)

	savePath := "./uploads/" + handler.Filename
	dst, err := os.Create(savePath)
	if err != nil {
		log.Println("❌ ファイル保存失敗:", err)
		http.Error(w, "保存失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Println("❌ ファイル書き込み失敗:", err)
		http.Error(w, "保存書き込み失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec(`
		INSERT INTO message_attachments (message_id, file_name, created_at)
		VALUES ($1, $2, $3)
	`, messageID, handler.Filename, time.Now())
	if err != nil {
		log.Println("❌ DB登録失敗:", err)
		http.Error(w, "DB登録失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("✅ アップロード成功:", savePath)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"urls": []string{"http://localhost:8080/uploads/" + handler.Filename},
	})
}
