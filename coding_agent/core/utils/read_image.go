package utils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif" // 注册 GIF decoder 供 image.Decode 使用
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// imageFormat 支持的图片格式及其 MIME 映射。
type imageFormat struct {
	MimeType   string
	Extensions []string
	Header     []byte // magic bytes 前缀
}

var supportedImageFormats = []imageFormat{
	{MimeType: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}, Header: []byte{0xff, 0xd8, 0xff}},
	{MimeType: "image/png", Extensions: []string{".png"}, Header: []byte{0x89, 0x50, 0x4e, 0x47}},
	{MimeType: "image/gif", Extensions: []string{".gif"}, Header: []byte{0x47, 0x49, 0x46}},
	{MimeType: "image/webp", Extensions: []string{".webp"}, Header: []byte{0x52, 0x49, 0x46, 0x46}},
}

// ResizedImage 保存缩放结果。
type ResizedImage struct {
	Data           string // base64
	MimeType       string
	OriginalWidth  int
	OriginalHeight int
	Width          int
	Height         int
	WasResized     bool
}

const (
	resizeMaxWidth    = 2000
	resizeMaxHeight   = 2000
	resizeMaxBase64   = 4.5 * 1024 * 1024 // 4.5MB base64（预留 0.5MB 给 API 限制）
	resizeJpegQuality = 80
	resizeScaleFactor = 0.75 // 每次缩放到 75%
)

// detectImageMimeType 通过 magic bytes 检测图片 MIME 类型，以扩展名做兜底。
func DetectImageMimeType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, 12)
	if _, err := f.Read(header); err != nil {
		return "", err
	}

	// magic bytes 匹配
	for _, format := range supportedImageFormats {
		if bytes.HasPrefix(header, format.Header) {
			return format.MimeType, nil
		}
	}

	// magic bytes 匹配失败，兜底检查扩展名
	ext := strings.ToLower(filepath.Ext(path))
	for _, format := range supportedImageFormats {
		for _, e := range format.Extensions {
			if ext == e {
				return format.MimeType, nil
			}
		}
	}

	return "", fmt.Errorf("unsupported image format: %s", ext)
}

// resizeImage 缩放图片到限制内。
//
// 策略：
//  1. 若原图尺寸和 base64 都已在限制内，直接返回原图
//  2. 缩放到 maxWidth/maxHeight 以内
//  3. 用 JPEG(quality=80,65,50) 编码，选能放下且最小的
//  4. 若都超，缩放到 75% 继续试，直到 1x1
//
// WebP 不做缩放（Go 无法 decode），直接以原始 base64 返回，wasResized=false。
func ResizeImage(data []byte, mimeType string) (*ResizedImage, error) {
	if mimeType == "image/webp" {
		// WebP 不做缩放，直接返回原始数据
		return &ResizedImage{
			Data:       base64.StdEncoding.EncodeToString(data),
			MimeType:   mimeType,
			WasResized: false,
		}, nil
	}

	// 解码原图
	src, formatName, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	// 计算 base64 大小
	origBase64Len := base64.StdEncoding.EncodedLen(len(data))

	// 已在所有限制内，直接返回
	if origW <= resizeMaxWidth && origH <= resizeMaxHeight && origBase64Len < resizeMaxBase64 {
		return &ResizedImage{
			Data:           base64.StdEncoding.EncodeToString(data),
			MimeType:       mimeType,
			OriginalWidth:  origW,
			OriginalHeight: origH,
			Width:          origW,
			Height:         origH,
			WasResized:     false,
		}, nil
	}

	// 需要缩放：目标尺寸
	targetW, targetH := fitSize(origW, origH, resizeMaxWidth, resizeMaxHeight)

	// 缩小后尝试编码，直到 1x1
	curW, curH := targetW, targetH
	for {
		// 缩放到当前尺寸
		dst := image.NewNRGBA(image.Rect(0, 0, curW, curH))
		draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

		// 尝试多种质量
		qualities := []int{resizeJpegQuality, 65, 50}
		candidates := make([]encodedCandidate, 0, len(qualities)+1)

		// JPEG 编码
		for _, q := range qualities {
			cand := tryEncodeJPEG(dst, q)
			candidates = append(candidates, cand)
		}

		// 如果是 PNG 且有透明通道，也尝试 PNG
		if formatName == "png" {
			cand := tryEncodePNG(dst)
			candidates = append(candidates, cand)
		}

		// 选能放下且最小的
		var best *encodedCandidate
		for _, cand := range candidates {
			if cand.encodedSize < resizeMaxBase64 {
				if best == nil || cand.encodedSize < best.encodedSize {
					c := cand
					best = &c
				}
			}
		}

		if best != nil {
			return &ResizedImage{
				Data:           best.data,
				MimeType:       best.mimeType,
				OriginalWidth:  origW,
				OriginalHeight: origH,
				Width:          curW,
				Height:         curH,
				WasResized:     origW != curW || origH != curH,
			}, nil
		}

		// 缩小再试
		if curW <= 1 && curH <= 1 {
			break
		}
		nextW := max(1, int(float64(curW)*resizeScaleFactor))
		nextH := max(1, int(float64(curH)*resizeScaleFactor))
		if nextW == curW && nextH == curH {
			break
		}
		curW, curH = nextW, nextH
	}

	return nil, nil
}

type encodedCandidate struct {
	data        string
	encodedSize int
	mimeType    string
}

func tryEncodeJPEG(img image.Image, quality int) encodedCandidate {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return encodedCandidate{encodedSize: -1}
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	return encodedCandidate{
		data:        data,
		encodedSize: len(data),
		mimeType:    "image/jpeg",
	}
}

func tryEncodePNG(img image.Image) encodedCandidate {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return encodedCandidate{encodedSize: -1}
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	return encodedCandidate{
		data:        data,
		encodedSize: len(data),
		mimeType:    "image/png",
	}
}

// fitSize 计算保持宽高比的缩放目标尺寸。
func fitSize(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	if w == 0 || h == 0 {
		return 1, 1
	}
	ratio := float64(w) / float64(h)
	if w > maxW {
		w = maxW
		h = int(float64(w) / ratio)
	}
	if h > maxH {
		h = maxH
		w = int(float64(h) * ratio)
	}
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return w, h
}

// formatDimensionNote 生成坐标映射说明。
func FormatDimensionNote(img *ResizedImage) string {
	if !img.WasResized {
		return ""
	}
	if img.OriginalWidth == 0 || img.Width == 0 {
		return ""
	}
	scale := float64(img.OriginalWidth) / float64(img.Width)
	return fmt.Sprintf("[Image: original %dx%d, displayed at %dx%d. Multiply coordinates by %.2f to map to original image.]",
		img.OriginalWidth, img.OriginalHeight, img.Width, img.Height, scale)
}

// isImageFile 检查路径是否为支持的图片文件。
func IsImageFile(path string) bool {
	// 先通过扩展名快速过滤
	ext := strings.ToLower(filepath.Ext(path))
	for _, format := range supportedImageFormats {
		for _, e := range format.Extensions {
			if ext == e {
				return true
			}
		}
	}
	return false
}

// 兼容旧代码的扩展名检查
var imageExtensions = map[string]bool{}

func init() {
	for _, format := range supportedImageFormats {
		for _, ext := range format.Extensions {
			imageExtensions[ext] = true
		}
	}
}
