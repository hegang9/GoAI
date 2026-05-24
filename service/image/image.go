package image

import (
	"GopherAI/common/image"
	"GopherAI/common/logger"
	"io"
	"mime/multipart"
)


func RecognizeImage(file *multipart.FileHeader) (string, error) {

	modelPath := "/root/models/mobilenetv2/mobilenetv2-7.onnx"
	labelPath := "/root/imagenet_classes.txt"
	inputH, inputW := 224, 224


	recognizer, err := image.NewImageRecognizer(modelPath, labelPath, inputH, inputW)
	if err != nil {
		logger.Error("NewImageRecognizer", "err", err)
		return "", err
	}
	defer recognizer.Close() 

	src, err := file.Open()
	if err != nil {
		logger.Error("file open", "err", err)
		return "", err
	}
	defer src.Close()

	buf, err := io.ReadAll(src)
	if err != nil {
		logger.Error("io.ReadAll", "err", err)
		return "", err
	}


	return recognizer.PredictFromBuffer(buf)
}
