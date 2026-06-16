// Package image 是图像识别适配层：基于 ONNX Runtime 实现 domain/image.Recognizer 端口。
package image

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	domainimage "GopherAI/internal/domain/image"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

const (
	defaultInputName  = "data"
	defaultOutputName = "mobilenetv20_output_flatten0_reshape0"
)

var (
	initOnce sync.Once
	initErr  error
)

// ONNXRecognizer 基于 ONNX 模型实现 domain/image.Recognizer 端口。
//
// 它持有模型与标签文件路径，在每次识别时创建一次性的推理会话，
// 兼容旧实现「每次请求加载模型」的行为。
type ONNXRecognizer struct {
	modelPath string
	labelPath string
	inputH    int
	inputW    int
}

// NewONNXRecognizer 创建图像识别器适配器。inputH/inputW <= 0 时默认 224x224。
func NewONNXRecognizer(modelPath, labelPath string, inputH, inputW int) *ONNXRecognizer {
	if inputH <= 0 || inputW <= 0 {
		inputH, inputW = 224, 224
	}
	return &ONNXRecognizer{modelPath: modelPath, labelPath: labelPath, inputH: inputH, inputW: inputW}
}

// 编译期断言：ONNXRecognizer 必须满足领域端口。
var _ domainimage.Recognizer = (*ONNXRecognizer)(nil)

// session 持有一次性的 ONNX 推理会话及其张量。
type session struct {
	sess         *ort.Session[float32]
	inputH       int
	inputW       int
	labels       []string
	inputTensor  *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]
}

// Recognize 对图片字节执行识别，返回最可能的类别名称。
func (r *ONNXRecognizer) Recognize(content []byte) (string, error) {
	s, err := r.newSession()
	if err != nil {
		return "", err
	}
	defer s.close()

	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("failed to decode image from buffer: %w", err)
	}
	return s.predict(img)
}

// newSession 初始化 ONNX 环境并创建一次性会话与张量。
func (r *ONNXRecognizer) newSession() (*session, error) {
	initOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("onnxruntime initialize error: %w", initErr)
	}

	inputShape := ort.NewShape(1, 3, int64(r.inputH), int64(r.inputW))
	inData := make([]float32, inputShape.FlattenedSize())
	inTensor, err := ort.NewTensor(inputShape, inData)
	if err != nil {
		return nil, fmt.Errorf("create input tensor failed: %w", err)
	}

	outShape := ort.NewShape(1, 1000)
	outTensor, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		inTensor.Destroy()
		return nil, fmt.Errorf("create output tensor failed: %w", err)
	}

	sess, err := ort.NewSession[float32](
		r.modelPath,
		[]string{defaultInputName},
		[]string{defaultOutputName},
		[]*ort.Tensor[float32]{inTensor},
		[]*ort.Tensor[float32]{outTensor},
	)
	if err != nil {
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, fmt.Errorf("create onnx session failed: %w", err)
	}

	labels, err := loadLabels(r.labelPath)
	if err != nil {
		sess.Destroy()
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, err
	}

	return &session{
		sess:         sess,
		inputH:       r.inputH,
		inputW:       r.inputW,
		labels:       labels,
		inputTensor:  inTensor,
		outputTensor: outTensor,
	}, nil
}

// close 释放会话持有的资源。
func (s *session) close() {
	if s.sess != nil {
		_ = s.sess.Destroy()
	}
	if s.inputTensor != nil {
		_ = s.inputTensor.Destroy()
	}
	if s.outputTensor != nil {
		_ = s.outputTensor.Destroy()
	}
}

// predict 对解码后的图片执行预处理与推理。
func (s *session) predict(img image.Image) (string, error) {
	resizedImg := image.NewRGBA(image.Rect(0, 0, s.inputW, s.inputH))
	draw.CatmullRom.Scale(resizedImg, resizedImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	h, w := s.inputH, s.inputW
	ch := 3
	data := make([]float32, h*w*ch)

	// 按 NCHW 布局提取像素并归一化到 0~1。
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := resizedImg.At(x, y)
			rr, gg, bb, _ := c.RGBA()
			data[y*w+x] = float32(rr>>8) / 255.0
			data[h*w+y*w+x] = float32(gg>>8) / 255.0
			data[2*h*w+y*w+x] = float32(bb>>8) / 255.0
		}
	}

	copy(s.inputTensor.GetData(), data)

	if err := s.sess.Run(); err != nil {
		return "", fmt.Errorf("onnx run error: %w", err)
	}

	outData := s.outputTensor.GetData()
	if len(outData) == 0 {
		return "", errors.New("empty output from model")
	}

	maxIdx := 0
	maxVal := outData[0]
	for i := 1; i < len(outData); i++ {
		if outData[i] > maxVal {
			maxVal = outData[i]
			maxIdx = i
		}
	}

	if maxIdx >= 0 && maxIdx < len(s.labels) {
		return s.labels[maxIdx], nil
	}
	return "Unknown", nil
}

// loadLabels 读取分类标签文件。
func loadLabels(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open label file failed: %w", err)
	}
	defer f.Close()

	var labels []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			labels = append(labels, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read labels failed: %w", err)
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels found in %s", path)
	}
	return labels, nil
}
