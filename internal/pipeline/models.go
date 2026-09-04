package pipeline

var (
	NODE_TYPE map[string]string = map[string]string{
		"concept":          "Абстрактное понятие, идея, явление (изоляция, абстракция, демократия)",
		"technology":       "Конкретный инструмент, технология, продукт, язык (Docker, Python, CRISPR)",
		"person":           "Человек, персона (Эйнштейн, Путин, Линус Торвальдс)",
		"organization":     "Компания, институт, государство (Google, ООН, МГУ)",
		"process":          "Процесс, алгоритм, процедура (компиляция, фотосинтез, выборы)",
		"property":         "Свойство, характеристика, атрибут (скорость, прозрачность, плотность)",
		"event":            "Событие, инцидент (революция, запуск продукта, открытие)",
		"location":         "Место, географический объект (Москва, Байкал, Марс)",
		"object":           "Физический объект, предмет (лампа, молекула, автомобиль)",
		"kernel_mechanism": "Механизм ядра ОС (cgroups, namespaces, syscalls)",
		"law":              "Закон, теорема, принцип, правило (закон Ньютона, принцип LSP)",
		"language":         "Язык (в широком смысле: программирования, человеческий)",
		"framework":        "Фреймворк, библиотека, набор инструментов (React, Pandas)",
		"protocol":         "Протокол, стандарт, спецификация (HTTP, TCP, OAuth)",
		"measure":          "Метрика, единица измерения (ватт, процент, IQ)",
	}

	EDGE_TYPE map[string]string = map[string]string{
		"is_a":           "X является Y (категоризация): Docker is_a платформа",
		"part_of":        "X часть Y: колесо part_of автомобиль",
		"uses":           "X использует Y: Docker uses cgroups",
		"causes":         "X вызывает Y: нагревание causes расширение",
		"produces":       "X производит Y: завод produces автомобили",
		"contrasts_with": "X противопоставляется Y: контейнер contrasts_with ВМ",
		"example_of":     "X пример Y: Docker example_of контейнеризация",
		"depends_on":     "X зависит от Y: Docker depends_on ядро Linux",
		"implements":     "X реализует Y: Docker implements спецификацию OCI",
		"has_property":   "X имеет свойство Y: контейнер has_property быстрый_запуск",
		"invented":       "X изобрёл Y: Эдисон invented лампа",
		"discovered":     "X открыл Y: Флеминг discovered пенициллин",
		"located_in":     "X находится в Y: Кремль located_in Москва",
		"related_to":     "X связан с Y (fallback, если не подходит ни один тип)",
	}
)

type DeepseekResponse struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Type       string         `json:"type"`
	Definition string         `json:"definition"`
	Aliases    []string       `json:"aliases"`
	Domain     string         `json:"domain"`
	Subtopic   string         `json:"subtopic"`
	Properties map[string]any `json:"properties"`
	KeyFacts   []string       `json:"key_facts"`
	Importance string         `json:"importance"` // high | medium | low
}

type Edge struct {
	Source     string  `json:"source"`     // ID узла-источника
	Target     string  `json:"target"`     // ID узла-цели
	Type       string  `json:"type"`       // is_a, part_of, uses, causes, produces, contrasts_with, example_of, depends_on, implements, has_property, invented, discovered, located_in, related_to
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	Evidence   string  `json:"evidence"`   // цитата или перефразирование из текста
}
