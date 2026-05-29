package image

import (
	"GopherAI/bo"
	"GopherAI/common/image"
	"GopherAI/common/logger"
	"io"
	"mime/multipart"
)

func RecognizeImage(file *multipart.FileHeader) (bo.ImageResultBO, error) {
	modelPath := "/root/models/mobilenetv2/mobilenetv2-7.onnx"
	labelPath := "/root/imagenet_classes.txt"
	inputH, inputW := 224, 224

	recognizer, err := image.NewImageRecognizer(modelPath, labelPath, inputH, inputW)
	if err != nil {
		logger.Error("NewImageRecognizer", "err", err)
		return bo.ImageResultBO{}, err
	}
	defer recognizer.Close()

	src, err := file.Open()
	if err != nil {
		logger.Error("file open", "err", err)
		return bo.ImageResultBO{}, err
	}
	defer src.Close()

	buf, err := io.ReadAll(src)
	if err != nil {
		logger.Error("io.ReadAll", "err", err)
		return bo.ImageResultBO{}, err
	}

	className, err := recognizer.PredictFromBuffer(buf)
	if err != nil {
		return bo.ImageResultBO{}, err
	}

	return bo.ImageResultBO{ClassName: className}, nil
}
