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

// ファイルアップロードを処理するHTTPハンドラ
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	// CORS（クロスオリジン）設定。全てのオリジンからのアクセスを許可
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// オプションメソッド（プリフライトリクエスト）の場合はここで終了
	if r.Method == http.MethodOptions {
		return
	}

	// multipart/form-dataのパース（最大10MBまで許容）
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		log.Println("❌ Formパース失敗:", err)
		http.Error(w, "ファイルパース失敗: "+err.Error(), http.StatusBadRequest)
		return
	}

	// フォームから message_id を取得し、整数へ変換
	messageIDStr := r.FormValue("message_id")
	messageID, err := strconv.Atoi(messageIDStr)
	if err != nil {
		log.Println("❌ message_idエラー:", err)
		http.Error(w, "message_idが不正です: "+err.Error(), http.StatusBadRequest)
		return
	}

	// フォームから画像ファイルを取得
	file, handler, err := r.FormFile("image")
	if err != nil {
		log.Println("❌ ファイル取得失敗:", err)
		http.Error(w, "ファイル取得失敗: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close() // 処理が終わったらファイルを閉じる

	log.Println("📎 アップロード開始: ファイル名 =", handler.Filename, ", message_id =", messageID)

	// ファイル保存先のパスを指定（例: ./uploads/filename.png）
	savePath := "./uploads/" + handler.Filename

	// 指定パスにファイルを作成
	dst, err := os.Create(savePath)
	if err != nil {
		log.Println("❌ ファイル保存失敗:", err)
		http.Error(w, "保存失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close() // 処理が終わったらファイルを閉じる

	// アップロードされたファイルの内容を保存先ファイルにコピー
	if _, err := io.Copy(dst, file); err != nil {
		log.Println("❌ ファイル書き込み失敗:", err)
		http.Error(w, "保存書き込み失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// DBにアップロードされたファイルのメタデータを登録
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

	// アップロードされたファイルのURLをJSON形式でレスポンスとして返す
	json.NewEncoder(w).Encode(map[string]string{
		"url": "http://localhost:8080/uploads/" + handler.Filename,
	})
}
