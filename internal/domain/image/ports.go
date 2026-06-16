// Package image 是图像识别领域：定义图像识别端口。
//
// 该包只声明契约，不依赖具体推理框架（ONNX）；实现位于 infrastructure/image。
package image

// Recognizer 定义图像识别端口：对图片字节执行分类并返回类别名称。
type Recognizer interface {
	// Recognize 对内存中的图片字节执行识别，返回最可能的类别名称。
	Recognize(content []byte) (className string, err error)
}
