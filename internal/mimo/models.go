package mimo

// Message 是单条消息（用于路由判断）
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string 或 []ContentPart
}

// ContentPart 是多模态内容块
type ContentPart struct {
	Type     string    `json:"type"` // text, image_url, file
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
	File     *FileData `json:"file,omitempty"`
}

// ImageURL 图片引用
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// FileData 文件数据
type FileData struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64
}
