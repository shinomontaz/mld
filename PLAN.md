# MLD Router Implementation Plan

Go-based Multi-Level Dijkstra (MLD) routing engine with dynamic profiles and incremental graph updates.

---

## 📋 План разработки (краткая сводка)

1. **OSM Data Loading** - Парсинг OSM PBF, построение node-based графа
2. **Profile Interface** - Интерфейс профилей маршрутизации (car, bike, walk)
3. **Graph Partitioning** - Разбиение графа на иерархию ячеек (grid + inertial flow)
4. **Customization** - Предвычисление структурных метрик (топология путей между boundary nodes)
5. **Query Algorithm** - Bidirectional MLD search с использованием метрик
6. **Dynamic Profiles** - Query-time применение профилей без re-customization
7. **Incremental Updates** - Кластеризация и точечное обновление при дорожных событиях

---

## 1. OSM Data Loading

### Цель
Загрузить OSM данные из `.osm.pbf` файла и построить граф дорожной сети.

### Задачи

#### 1.1. Выбор OSM экстракта (1 день)
**Что делать:**
- Скачать тестовый датасет Monaco (~1MB) с geofabrik.de
- Альтернативно: любой небольшой город для тестов

**Ссылка:**
```
https://download.geofabrik.de/europe/monaco-latest.osm.pbf
```

#### 1.2. OSM Parser (3-4 дня)
**Библиотека:** `github.com/paulmach/osm`

**Структуры данных:**
```go
// pkg/osm/parser.go
type OSMParser struct {
    Nodes map[int64]*osm.Node      // OSM node ID -> координаты
    Ways  map[int64]*osm.Way       // OSM way ID -> список node IDs
}

func ParsePBF(filename string) (*OSMParser, error) {
    // Парсинг PBF файла
    // Фильтрация: только highway=* (дороги)
    // Исключить: footway, path, cycleway (опционально)
}
```

**Фильтры:**
- `highway=motorway|trunk|primary|secondary|tertiary|residential|service`
- Исключить: `access=no`, `area=yes`

#### 1.3. Graph Builder (3-4 дня)
**Что делать:**
- Преобразовать OSM ways в рёбра графа
- Создать node-based граф (вершины = перекрёстки)
- Вычислить расстояния по координатам (Haversine formula)

**Ссылки на OSRM код:**
- Adjacency list (FirstEdge): `include/util/static_graph.hpp:44-48, 169-177`
- Построение FirstEdge: `include/util/static_graph.hpp:286-314`
- Edge направленность (forward/backward): `include/extractor/edge_based_edge.hpp:14-17`
- NodeBasedEdgeData: `include/util/node_based_graph.hpp:15-43`

**Структуры:**
```go
// pkg/graph/graph.go
type Graph struct {
    NumNodes  int
    NumEdges  int
    Nodes     []Node
    Edges     []Edge
    
    // Adjacency list для быстрого доступа к рёбрам
    // OSRM: include/util/static_graph.hpp:44-48
    // Хранит индекс первого ребра для каждой вершины
    // Рёбра вершины n: Edges[FirstEdge[n]:FirstEdge[n+1]]
    FirstEdge []int  // [nodeID] -> индекс первого ребра в Edges
}

type Node struct {
    ID       NodeID
    Lat, Lon float64
    OSMId    int64  // Для отладки
}

type Edge struct {
    ID       EdgeID
    Source   NodeID
    Target   NodeID
    Distance float64  // Метры (Haversine)
    
    // КРИТИЧНО: Направленность ребра
    // OSRM: include/extractor/edge_based_edge.hpp:14-17
    Forward  bool  // Можно ли ехать Source → Target
    Backward bool  // Можно ли ехать Target → Source
    
    OSMWayID int64    // Для отладки
    Tags     map[string]string  // highway, maxspeed, oneway, access, etc.
}
```

**Использование adjacency list:**
```go
// Получить исходящие рёбра вершины (как в OSRM)
func (g *Graph) GetOutgoingEdges(nodeID NodeID) []Edge {
    start := g.FirstEdge[nodeID]
    end := g.FirstEdge[nodeID+1]
    return g.Edges[start:end]
}

// Пример работы:
// Граф с 5 вершинами (0-4), рёбра после сортировки:
//   [0] {Source: 0, Target: 2}
//   [1] {Source: 0, Target: 3}
//   [2] {Source: 2, Target: 4}  ← Вершина 1 без исходящих рёбер
//   [3] {Source: 3, Target: 1}
//   [4] {Source: 4, Target: 0}
//
// Построение FirstEdge:
//   FirstEdge[0] = 0  → GetOutgoingEdges(0) = Edges[0:2] = [{0→2}, {0→3}]
//   FirstEdge[1] = 2  → GetOutgoingEdges(1) = Edges[2:2] = [] (пусто!)
//   FirstEdge[2] = 2  → GetOutgoingEdges(2) = Edges[2:3] = [{2→4}]
//   FirstEdge[3] = 3  → GetOutgoingEdges(3) = Edges[3:4] = [{3→1}]
//   FirstEdge[4] = 4  → GetOutgoingEdges(4) = Edges[4:5] = [{4→0}]
//   FirstEdge[5] = 5  → Конец массива
}

// Построение adjacency list после загрузки графа
// OSRM: include/util/static_graph.hpp:286-308 (InitializeFromSortedEdgeRange)
func (g *Graph) BuildAdjacencyList() {
    // 1. Сортируем рёбра по Source (затем по Target для детерминизма)
    sort.Slice(g.Edges, func(i, j int) bool {
        if g.Edges[i].Source != g.Edges[j].Source {
            return g.Edges[i].Source < g.Edges[j].Source
        }
        return g.Edges[i].Target < g.Edges[j].Target
    })
    
    // 2. Создаём FirstEdge массив (размер = NumNodes + 1)
    // FirstEdge[node] = индекс первого ребра вершины node
    // FirstEdge[node+1] = индекс первого ребра следующей вершины
    // Рёбра вершины node: Edges[FirstEdge[node]:FirstEdge[node+1]]
    g.FirstEdge = make([]int, g.NumNodes+1)
    g.FirstEdge[0] = 0
    
    // 3. Заполняем индексы (логика OSRM)
    edgeIdx := 0
    for nodeID := 0; nodeID < g.NumNodes; nodeID++ {
        // Пропускаем все рёбра текущей вершины
        for edgeIdx < len(g.Edges) && g.Edges[edgeIdx].Source == NodeID(nodeID) {
            edgeIdx++
        }
        // Записываем индекс для следующей вершины
        // Если у вершины nodeID нет рёбер: FirstEdge[nodeID] == FirstEdge[nodeID+1]
        g.FirstEdge[nodeID+1] = edgeIdx
    }
    
    // Проверка: последний элемент должен быть равен количеству рёбер
    if g.FirstEdge[g.NumNodes] != len(g.Edges) {
        panic(fmt.Sprintf("BuildAdjacencyList: expected FirstEdge[%d]=%d, got %d",
            g.NumNodes, len(g.Edges), g.FirstEdge[g.NumNodes]))
    }
}
```

**Обработка направленности (oneway):**
```go
// pkg/osm/way_utils.go
func (w *Way) IsOneway() bool {
    if oneway, ok := w.Tags["oneway"]; ok {
        return oneway == "yes" || oneway == "1" || oneway == "true"
    }
    // Некоторые типы дорог всегда oneway
    if highway, ok := w.Tags["highway"]; ok {
        return highway == "motorway" || highway == "motorway_link"
    }
    return false
}

func (w *Way) IsReversed() bool {
    if oneway, ok := w.Tags["oneway"]; ok {
        return oneway == "-1" || oneway == "reverse"
    }
    return false
}

func (w *Way) IsAccessible() bool {
    if access, ok := w.Tags["access"]; ok {
        return access != "no" && access != "private"
    }
    return true
}
```

**Создание рёбер с учётом направленности:**
```go
// pkg/osm/graph_builder.go
func createEdgeFromWaySegment(way *Way, segment Segment, distance float64) Edge {
    edge := Edge{
        Source:   segment.StartNode,
        Target:   segment.EndNode,
        Distance: distance,
        OSMWayID: way.Id,
        Tags:     way.Tags,
    }
    
    // Определяем направленность
    if !way.IsAccessible() {
        // Недоступная дорога - оба направления false
        edge.Forward = false
        edge.Backward = false
    } else if way.IsOneway() {
        if way.IsReversed() {
            // oneway=-1: только назад
            edge.Forward = false
            edge.Backward = true
        } else {
            // oneway=yes: только вперёд
            edge.Forward = true
            edge.Backward = false
        }
    } else {
        // Двунаправленная дорога
        edge.Forward = true
        edge.Backward = true
    }
    
    return edge
}
```

**Формула расстояния:**
```go
// pkg/graph/distance.go
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
    const R = 6371000.0 // Радиус Земли в метрах
    
    dLat := (lat2 - lat1) * math.Pi / 180.0
    dLon := (lon2 - lon1) * math.Pi / 180.0
    
    lat1Rad := lat1 * math.Pi / 180.0
    lat2Rad := lat2 * math.Pi / 180.0
    
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
         math.Cos(lat1Rad)*math.Cos(lat2Rad)*
         math.Sin(dLon/2)*math.Sin(dLon/2)
    
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    
    return R * c
}
```

#### 1.4. Сериализация графа (2 дня)
**Формат:** Binary (custom) или Protocol Buffers

```go
// pkg/storage/graph_storage.go
func SaveGraph(g *graph.Graph, filename string) error
func LoadGraph(filename string) (*graph.Graph, error)
```

**Файлы:**
- `graph.bin` - граф
- `graph.meta` - метаданные (количество вершин, рёбер, bounding box)

#### 1.5. Тестирование (1 день)
**Проверки:**
- Количество вершин и рёбер соответствует ожиданиям
- Граф связный (можно добраться из любой вершины в любую)
- Координаты корректны (внутри bounding box)

**Утилита:**
```bash
# cmd/osm-import/main.go
go run cmd/osm-import/main.go \
    -input monaco.osm.pbf \
    -output data/monaco.graph.bin
```

---

## 2. Profile Interface

### Цель
Разработать интерфейс профилей маршрутизации для разных видов транспорта.

### Задачи

#### 2.1. Базовый интерфейс (2 дня)
```go
// pkg/profile/profile.go
type Direction int

const (
    Forward Direction = iota  // Source → Target
    Backward                   // Target → Source
)

type Profile interface {
    // Вес ребра (время в секундах) с учётом направления
    EdgeWeight(edge *graph.Edge, direction Direction) float64
    
    // Название профиля
    Name() string
    
    // Допустимо ли ребро для этого профиля в данном направлении
    IsAccessible(edge *graph.Edge, direction Direction) bool
}
```

#### 2.2. Simple Profile (1 день)
**Для тестирования:**
```go
type SimpleProfile struct {
    DefaultSpeed float64  // км/ч
}

func (p *SimpleProfile) EdgeWeight(edge *graph.Edge, direction Direction) float64 {
    // Проверка доступности направления
    if !p.IsAccessible(edge, direction) {
        return math.Inf(1)  // Недоступно
    }
    
    // Время = расстояние / скорость
    speedMPS := p.DefaultSpeed / 3.6  // км/ч -> м/с
    return edge.Distance / speedMPS
}

func (p *SimpleProfile) IsAccessible(edge *graph.Edge, direction Direction) bool {
    // Проверяем флаги Forward/Backward
    if direction == Forward {
        return edge.Forward
    }
    return edge.Backward
}

func (p *SimpleProfile) Name() string {
    return "simple"
}
```

#### 2.3. Car Profile (3 дня)
**С учётом типов дорог:**
```go
type CarProfile struct {
    Speeds map[string]float64  // highway type -> speed (км/ч)
}

func NewCarProfile() *CarProfile {
    return &CarProfile{
        Speeds: map[string]float64{
            "motorway":    120.0,
            "trunk":       100.0,
            "primary":     80.0,
            "secondary":   60.0,
            "tertiary":    50.0,
            "residential": 30.0,
            "service":     20.0,
        },
    }
}

func (p *CarProfile) EdgeWeight(edge *graph.Edge, direction Direction) float64 {
    // Проверка доступности направления
    if !p.IsAccessible(edge, direction) {
        return math.Inf(1)  // Недоступно
    }
    
    highway := edge.Tags["highway"]
    speed := p.Speeds[highway]
    if speed == 0 {
        speed = 50.0  // default
    }
    
    // Проверка maxspeed tag
    if maxspeed := edge.Tags["maxspeed"]; maxspeed != "" {
        if parsed := parseSpeed(maxspeed); parsed > 0 {
            speed = min(speed, parsed)
        }
    }
    
    speedMPS := speed / 3.6
    return edge.Distance / speedMPS
}

func (p *CarProfile) IsAccessible(edge *graph.Edge, direction Direction) bool {
    // Проверка направленности
    if direction == Forward && !edge.Forward {
        return false
    }
    if direction == Backward && !edge.Backward {
        return false
    }
    
    // Проверка access tags
    if edge.Tags["access"] == "no" {
        return false
    }
    if edge.Tags["motor_vehicle"] == "no" {
        return false
    }
    return true
}
```

#### 2.4. Bike/Walk Profiles (опционально, 2 дня)
**Для будущего тестирования динамических профилей.**

#### 2.5. Profile Loader (2 дня)
**Загрузка из конфигурации:**
```go
// profiles/car.yaml
name: car
type: speed_based
speeds:
  motorway: 120
  trunk: 100
  primary: 80
  # ...
restrictions:
  - tag: access
    value: no
    action: deny
```

```go
func LoadProfileFromYAML(filename string) (Profile, error)
```

---

## 3. Graph Partitioning

### Цель
Разбить граф на иерархию ячеек для MLD.

### Задачи

#### 3.1. Multi-Level Partition структура (2 дня)

**Ссылки на OSRM код:**
- `GetHighestDifferentLevel`: `include/partitioner/multi_level_partition.hpp:109-116`
- `BoundaryNodes` (source/destination): `include/partitioner/cell_storage.hpp:58-65, 85-86`
- `CellIDs` (partition array): `include/partitioner/multi_level_partition.hpp:49-60`
- `findBoundaryNodes`: `include/partitioner/cell_storage.hpp:254-289`

```go
// pkg/partition/partition.go
type MultiLevelPartition struct {
    NumLevels int
    NumCells  []int        // [level] -> количество ячеек на уровне
    
    // Назначение ячеек вершинам
    // Предполагаем NodeID последовательные 0..N-1
    // OSRM: include/partitioner/multi_level_partition.hpp:49-60
    CellIDs   [][]CellID   // [level][nodeID] -> cellID
    
    // Boundary nodes для каждой ячейки
    // OSRM: include/partitioner/cell_storage.hpp:58-65
    Boundaries map[LevelCellKey]*BoundaryNodes
}

type LevelCellKey struct {
    Level  LevelID
    CellID CellID
}

type BoundaryNodes struct {
    // Для упрощённой версии (ненаправленный граф):
    Nodes []NodeID  // Все boundary nodes
    
    // Для полной версии (направленный граф):
    // OSRM: include/partitioner/cell_storage.hpp:85-86
    // Sources []NodeID  // Вершины с исходящими рёбрами в другие ячейки
    // Targets []NodeID  // Вершины с входящими рёбрами из других ячеек
}

// Основные методы
func (p *MultiLevelPartition) GetCell(level LevelID, node NodeID) CellID {
    return p.CellIDs[level][node]
}

func (p *MultiLevelPartition) GetBoundaryNodes(level LevelID, cell CellID) *BoundaryNodes {
    key := LevelCellKey{Level: level, CellID: cell}
    return p.Boundaries[key]
}

// КРИТИЧЕСКИЙ метод для MLD query!
// OSRM: include/partitioner/multi_level_partition.hpp:109-116
func (p *MultiLevelPartition) GetHighestDifferentLevel(source, target NodeID) LevelID {
    // Найти максимальный уровень, где source и target в РАЗНЫХ ячейках
    // Это определяет, на каком уровне можно использовать ребро
    // 
    // OSRM использует битовые операции для оптимизации:
    // auto msb = util::msb(partition[first] ^ partition[second]);
    // return level_data->bit_to_level[msb];
    //
    // Упрощённая версия:
    for level := LevelID(p.NumLevels); level >= 1; level-- {
        if p.GetCell(level, source) != p.GetCell(level, target) {
            return level
        }
    }
    return 0  // В одной ячейке на всех уровнях (не должно использоваться в query)
}

// Для определения уровня встречи в bidirectional search
// OSRM: include/partitioner/multi_level_partition.hpp:103-107
func (p *MultiLevelPartition) GetQueryLevel(source, target NodeID) LevelID {
    // Найти минимальный уровень, где source и target в разных ячейках
    // OSRM: return std::min(GetHighestDifferentLevel(start, node),
    //                       GetHighestDifferentLevel(target, node));
    for level := LevelID(1); level <= LevelID(p.NumLevels); level++ {
        if p.GetCell(level, source) != p.GetCell(level, target) {
            return level
        }
    }
    return LevelID(p.NumLevels)
}
```

**Важные замечания:**
1. **CellIDs** - предполагаем, что NodeID последовательные (0..N-1). Если нет, нужно использовать `map[NodeID]CellID`.
2. **BoundaryNodes** - для начала используем упрощённую версию (все boundary nodes в одном списке). Для направленного графа нужно разделить на Sources/Targets как в OSRM.
3. **GetHighestDifferentLevel** - критический метод для определения, можно ли использовать ребро на данном уровне query. OSRM использует битовые операции для оптимизации, мы используем простой цикл.

#### 3.2. Grid-based Partitioning (3 дня)

**Ссылки на OSRM код:**
- Алгоритм определения boundary nodes: `include/partitioner/cell_storage.hpp:254-289`
- Разделение на source/destination: `include/partitioner/cell_storage.hpp:266-277`

**Простой алгоритм для начала:**
```go
// pkg/partition/grid.go
type GridPartitioner struct {
    NumLevels int
    GridSizes []int  // [level] -> размер сетки (например, [4, 16, 64])
}

func (p *GridPartitioner) Partition(g *graph.Graph) *MultiLevelPartition {
    mlp := &MultiLevelPartition{
        NumLevels:  p.NumLevels,
        NumCells:   make([]int, p.NumLevels+1),  // +1 для индексации с 1
        CellIDs:    make([][]CellID, p.NumLevels+1),
        Boundaries: make(map[LevelCellKey]*BoundaryNodes),
    }
    
    // Найти bounding box
    minLat, maxLat := math.Inf(1), math.Inf(-1)
    minLon, maxLon := math.Inf(1), math.Inf(-1)
    
    for _, node := range g.Nodes {
        minLat = math.Min(minLat, node.Lat)
        maxLat = math.Max(maxLat, node.Lat)
        minLon = math.Min(minLon, node.Lon)
        maxLon = math.Max(maxLon, node.Lon)
    }
    
    // Для каждого уровня
    for level := 1; level <= p.NumLevels; level++ {
        gridSize := p.GridSizes[level-1]
        mlp.NumCells[level] = gridSize * gridSize
        mlp.CellIDs[level] = make([]CellID, g.NumNodes)
        
        cellWidth := (maxLon - minLon) / float64(gridSize)
        cellHeight := (maxLat - minLat) / float64(gridSize)
        
        // Назначить каждой вершине CellID
        for _, node := range g.Nodes {
            cellX := int((node.Lon - minLon) / cellWidth)
            cellY := int((node.Lat - minLat) / cellHeight)
            
            // Граничные случаи
            if cellX >= gridSize {
                cellX = gridSize - 1
            }
            if cellY >= gridSize {
                cellY = gridSize - 1
            }
            
            cellID := CellID(cellY*gridSize + cellX)
            mlp.CellIDs[level][node.ID] = cellID
        }
        
        // Определить boundary nodes для каждой ячейки
        p.findBoundaryNodes(g, mlp, LevelID(level))
    }
    
    return mlp
}

// Алгоритм определения boundary nodes
// OSRM: include/partitioner/cell_storage.hpp:254-289
func (p *GridPartitioner) findBoundaryNodes(
    g *graph.Graph,
    mlp *MultiLevelPartition,
    level LevelID,
) {
    // Для каждой ячейки собираем boundary nodes
    cellBoundaries := make(map[CellID]map[NodeID]bool)
    
    for cellID := CellID(0); cellID < CellID(mlp.NumCells[level]); cellID++ {
        cellBoundaries[cellID] = make(map[NodeID]bool)
    }
    
    // ВАЖНО: Проходим по всем РЁБРАМ (не вершинам!)
    // OSRM: for (auto edge : base_graph.GetAdjacentEdgeRange(node))
    for _, edge := range g.Edges {
        sourceCellID := mlp.CellIDs[level][edge.Source]
        targetCellID := mlp.CellIDs[level][edge.Target]
        
        // Ребро пересекает границу ячейки?
        // OSRM: is_boundary_node |= partition.GetCell(level, other) != cell_id;
        if sourceCellID != targetCellID {
            cellBoundaries[sourceCellID][edge.Source] = true
            cellBoundaries[targetCellID][edge.Target] = true
        }
    }
    
    // Конвертируем в BoundaryNodes
    for cellID, boundarySet := range cellBoundaries {
        if len(boundarySet) == 0 {
            continue
        }
        
        nodes := make([]NodeID, 0, len(boundarySet))
        for nodeID := range boundarySet {
            nodes = append(nodes, nodeID)
        }
        
        // Сортируем для детерминированности
        sort.Slice(nodes, func(i, j int) bool {
            return nodes[i] < nodes[j]
        })
        
        key := LevelCellKey{Level: level, CellID: cellID}
        mlp.Boundaries[key] = &BoundaryNodes{Nodes: nodes}
    }
}
```

**Для направленного графа (полная версия):**
```go
// OSRM: include/partitioner/cell_storage.hpp:266-277
func (p *GridPartitioner) findBoundaryNodesDirected(
    g *graph.Graph,
    mlp *MultiLevelPartition,
    level LevelID,
) {
    cellSources := make(map[CellID]map[NodeID]bool)
    cellTargets := make(map[CellID]map[NodeID]bool)
    
    for cellID := CellID(0); cellID < CellID(mlp.NumCells[level]); cellID++ {
        cellSources[cellID] = make(map[NodeID]bool)
        cellTargets[cellID] = make(map[NodeID]bool)
    }
    
    for _, edge := range g.Edges {
        sourceCellID := mlp.CellIDs[level][edge.Source]
        targetCellID := mlp.CellIDs[level][edge.Target]
        
        if sourceCellID != targetCellID {
            // OSRM: is_source_node |= partition.GetCell(level, other) == cell_id && data.forward;
            if edge.Forward {
                cellSources[sourceCellID][edge.Source] = true
            }
            
            // OSRM: is_destination_node |= partition.GetCell(level, other) == cell_id && data.backward;
            if edge.Backward {
                cellTargets[targetCellID][edge.Target] = true
            }
        }
    }
    
    // Конвертируем в BoundaryNodes с разделением на Sources/Targets
    for cellID := CellID(0); cellID < CellID(mlp.NumCells[level]); cellID++ {
        sources := make([]NodeID, 0, len(cellSources[cellID]))
        for nodeID := range cellSources[cellID] {
            sources = append(sources, nodeID)
        }
        
        targets := make([]NodeID, 0, len(cellTargets[cellID]))
        for nodeID := range cellTargets[cellID] {
            targets = append(targets, nodeID)
        }
        
        if len(sources) == 0 && len(targets) == 0 {
            continue
        }
        
        sort.Slice(sources, func(i, j int) bool { return sources[i] < sources[j] })
        sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
        
        key := LevelCellKey{Level: level, CellID: cellID}
        mlp.Boundaries[key] = &BoundaryNodes{
            Sources: sources,
            Targets: targets,
        }
    }
}
```

**Пример использования:**
```go
partitioner := &GridPartitioner{
    NumLevels: 3,
    GridSizes: []int{4, 8, 16},  // 16, 64, 256 ячеек
}

partition := partitioner.Partition(graph)

// Проверка
fmt.Printf("Level 1: %d cells\n", partition.NumCells[1])
fmt.Printf("Level 2: %d cells\n", partition.NumCells[2])
fmt.Printf("Level 3: %d cells\n", partition.NumCells[3])

// Статистика boundary nodes
for level := 1; level <= partition.NumLevels; level++ {
    totalBoundary := 0
    for cellID := 0; cellID < partition.NumCells[level]; cellID++ {
        bounds := partition.GetBoundaryNodes(LevelID(level), CellID(cellID))
        if bounds != nil {
            totalBoundary += len(bounds.Nodes)
        }
    }
    fmt.Printf("Level %d: %d boundary nodes (%.1f%%)\n", 
        level, totalBoundary, 100.0*float64(totalBoundary)/float64(graph.NumNodes))
}
```

#### 3.3. Inertial Flow Partitioning (опционально, 7-10 дней)
**Для лучшего качества разбиения.**

Можно отложить на потом, grid-based достаточно для PoC.

#### 3.4. Визуализация (2 дня)
**GeoJSON export для проверки:**
```go
// pkg/partition/export.go
func ExportToGeoJSON(p *MultiLevelPartition, g *graph.Graph, level LevelID) string {
    // Для каждой ячейки:
    // - Создать Polygon из выпуклой оболочки вершин
    // - Boundary nodes отметить как Point features
    // - Разные цвета для разных ячеек
}
```

**Проверка в QGIS или geojson.io:**
```bash
go run cmd/partition-viz/main.go \
    -graph data/monaco.graph.bin \
    -level 1 \
    -output viz/monaco_level1.geojson
```

#### 3.5. Метрики качества (1 день)
```go
func AnalyzePartition(p *MultiLevelPartition, g *graph.Graph) {
    for level := 1; level <= p.NumLevels; level++ {
        // Количество ячеек
        // Средний размер ячейки (вершин)
        // Количество boundary nodes (%)
        // Количество граничных рёбер (edge cut)
    }
}
```

**Цель:** Boundary nodes < 20% от общего числа вершин.

---

## 4. Customization

### Цель
Предвычислить структурные метрики - топологию кратчайших путей между boundary nodes.

### Задачи

#### 4.1. Cell Metrics структура (2 дня)
```go
// pkg/customize/metrics.go
type CellMetrics struct {
    Level  LevelID
    CellID CellID
    
    BoundaryNodes []NodeID
    
    // Пути между boundary nodes
    Paths [][]EdgePath  // [srcIdx][dstIdx] -> путь
    
    // Индекс рёбер (для инкрементальных обновлений)
    EdgeIndex map[EdgeID][]PathRef
}

type EdgePath struct {
    Edges    []EdgeID  // Последовательность рёбер
    Distance float64   // Физическое расстояние (не зависит от профиля)
}

type PathRef struct {
    SourceIdx int
    DestIdx   int
}

type MetricsStorage struct {
    Metrics map[LevelCellKey]*CellMetrics
}
```

#### 4.2. Dijkstra внутри ячейки (3 дня)
```go
// pkg/customize/dijkstra.go
func dijkstraInCell(
    g *graph.Graph,
    source NodeID,
    targets []NodeID,
    level LevelID,
    cellID CellID,
    partition *MultiLevelPartition,
) map[NodeID]EdgePath {
    
    heap := NewHeap()
    heap.Insert(source, 0.0)
    
    distances := make(map[NodeID]float64)
    predecessors := make(map[NodeID]EdgeID)  // Для восстановления пути
    
    remaining := len(targets)
    
    for !heap.Empty() && remaining > 0 {
        node, dist := heap.ExtractMin()
        distances[node] = dist
        
        // Нашли target?
        if isTarget(node, targets) {
            remaining--
        }
        
        // Релаксация
        for _, edge := range g.GetEdges(node) {
            target := edge.Target
            
            // ВАЖНО: не выходим за пределы ячейки
            if partition.GetCell(level, target) != cellID {
                continue
            }
            
            newDist := dist + edge.Distance  // Используем физическое расстояние
            
            if !heap.Contains(target) {
                heap.Insert(target, newDist)
                predecessors[target] = edge.ID
            } else if newDist < heap.GetKey(target) {
                heap.DecreaseKey(target, newDist)
                predecessors[target] = edge.ID
            }
        }
    }
    
    // Восстановление путей
    paths := make(map[NodeID]EdgePath)
    for _, targetNode := range targets {
        path := reconstructPath(source, targetNode, predecessors)
        paths[targetNode] = path
    }
    
    return paths
}
```

#### 4.3. Customization алгоритм (4 дня)
```go
// pkg/customize/customizer.go
type Customizer struct {
    Graph     *graph.Graph
    Partition *MultiLevelPartition
}

func (c *Customizer) Customize(level LevelID, cellID CellID) *CellMetrics {
    bounds := c.Partition.GetBoundaryNodes(level, cellID)
    
    metrics := &CellMetrics{
        Level:         level,
        CellID:        cellID,
        BoundaryNodes: bounds.Nodes,
        Paths:         make([][]EdgePath, len(bounds.Nodes)),
        EdgeIndex:     make(map[EdgeID][]PathRef),
    }
    
    // Для каждой boundary вершины
    for srcIdx, srcNode := range bounds.Nodes {
        metrics.Paths[srcIdx] = make([]EdgePath, len(bounds.Nodes))
        
        // Dijkstra от srcNode до всех остальных boundary nodes
        paths := dijkstraInCell(
            c.Graph,
            srcNode,
            bounds.Nodes,
            level,
            cellID,
            c.Partition,
        )
        
        // Сохраняем пути
        for dstIdx, dstNode := range bounds.Nodes {
            if srcNode == dstNode {
                continue  // Пропускаем путь к самому себе
            }
            
            path := paths[dstNode]
            metrics.Paths[srcIdx][dstIdx] = path
            
            // Индексируем рёбра
            for _, edgeID := range path.Edges {
                metrics.EdgeIndex[edgeID] = append(
                    metrics.EdgeIndex[edgeID],
                    PathRef{SourceIdx: srcIdx, DestIdx: dstIdx},
                )
            }
        }
    }
    
    return metrics
}
```

#### 4.4. Параллелизация (2 дня)
```go
func (c *Customizer) CustomizeLevel(level LevelID) *MetricsStorage {
    numCells := c.Partition.NumCells[level]
    storage := NewMetricsStorage()
    
    var wg sync.WaitGroup
    results := make(chan *CellMetrics, numCells)
    
    for cellID := 0; cellID < numCells; cellID++ {
        wg.Add(1)
        go func(cid CellID) {
            defer wg.Done()
            metrics := c.Customize(level, cid)
            results <- metrics
        }(CellID(cellID))
    }
    
    go func() {
        wg.Wait()
        close(results)
    }()
    
    for metrics := range results {
        storage.Set(metrics)
    }
    
    return storage
}
```

#### 4.5. Сериализация метрик (2 дня)
```go
// pkg/storage/metrics_storage.go
func SaveMetrics(metrics *MetricsStorage, filename string) error
func LoadMetrics(filename string) (*MetricsStorage, error)
```

**Формат:** Binary с компрессией (gzip).

**Файлы:**
- `metrics_level1.bin.gz`
- `metrics_level2.bin.gz`
- ...

#### 4.6. Тестирование (2 дня)
**Проверки:**
- Все пути между boundary nodes найдены
- Пути корректны (можно пройти по рёбрам)
- Размер метрик приемлем (< 100MB для Monaco)

---

## 5. Query Algorithm

### Цель
Реализовать быстрый поиск кратчайшего пути с использованием MLD метрик.

### Задачи

#### 5.1. Определение уровня встречи (1 день)
```go
// pkg/query/mld.go
func (p *MultiLevelPartition) GetQueryLevel(source, target NodeID) LevelID {
    // Найти минимальный уровень, где source и target в разных ячейках
    for level := LevelID(1); level <= LevelID(p.NumLevels); level++ {
        if p.GetCell(level, source) != p.GetCell(level, target) {
            return level
        }
    }
    return LevelID(p.NumLevels)
}
```

#### 5.2. Базовый Dijkstra для сравнения (2 дня)
**Для тестирования корректности:**
```go
func Dijkstra(g *graph.Graph, source, target NodeID) (distance float64, path []NodeID) {
    // Классический Dijkstra
    // Используем физические расстояния
}
```

#### 5.3. MLD Query (5-7 дней)
```go
type MLDQuery struct {
    Graph     *graph.Graph
    Partition *MultiLevelPartition
    Metrics   *MetricsStorage
}

func (q *MLDQuery) Route(source, target NodeID) (distance float64, path []NodeID) {
    queryLevel := q.Partition.GetQueryLevel(source, target)
    
    // Bidirectional search
    fwdHeap := NewHeap()
    revHeap := NewHeap()
    
    fwdHeap.Insert(source, 0.0)
    revHeap.Insert(target, 0.0)
    
    fwdDistances := make(map[NodeID]float64)
    revDistances := make(map[NodeID]float64)
    
    fwdPredecessors := make(map[NodeID]NodeID)
    revPredecessors := make(map[NodeID]NodeID)
    
    bestDistance := math.Inf(1)
    meetingNode := NodeID(0)
    
    for !fwdHeap.Empty() || !revHeap.Empty() {
        // Stopping criterion
        if fwdHeap.MinKey() + revHeap.MinKey() >= bestDistance {
            break
        }
        
        // Forward step
        if !fwdHeap.Empty() {
            node, dist := fwdHeap.ExtractMin()
            fwdDistances[node] = dist
            
            // Проверка встречи
            if revDist, found := revDistances[node]; found {
                totalDist := dist + revDist
                if totalDist < bestDistance {
                    bestDistance = totalDist
                    meetingNode = node
                }
            }
            
            // Релаксация
            q.relax(node, dist, queryLevel, fwdHeap, fwdDistances, fwdPredecessors, false)
        }
        
        // Reverse step (аналогично)
        if !revHeap.Empty() {
            node, dist := revHeap.ExtractMin()
            revDistances[node] = dist
            
            if fwdDist, found := fwdDistances[node]; found {
                totalDist := dist + fwdDist
                if totalDist < bestDistance {
                    bestDistance = totalDist
                    meetingNode = node
                }
            }
            
            q.relax(node, dist, queryLevel, revHeap, revDistances, revPredecessors, true)
        }
    }
    
    // Восстановление пути
    path = q.reconstructPath(source, target, meetingNode, fwdPredecessors, revPredecessors)
    return bestDistance, path
}
```

#### 5.4. Релаксация через метрики (4 дня)
```go
func (q *MLDQuery) relax(
    node NodeID,
    dist float64,
    queryLevel LevelID,
    heap *Heap,
    distances map[NodeID]float64,
    predecessors map[NodeID]NodeID,
    reverse bool,
) {
    // 1. Релаксация через рёбра базового графа
    edges := q.Graph.GetEdges(node)
    if reverse {
        edges = q.Graph.GetIncomingEdges(node)  // Нужно добавить в Graph
    }
    
    for _, edge := range edges {
        target := edge.Target
        if reverse {
            target = edge.Source
        }
        
        // Проверка: можно ли использовать это ребро?
        edgeLevel := q.getEdgeLevel(edge.Source, edge.Target)
        if edgeLevel < queryLevel {
            continue  // Ребро внутри ячейки нижнего уровня
        }
        
        newDist := dist + edge.Distance
        if _, visited := distances[target]; visited {
            continue
        }
        
        if !heap.Contains(target) {
            heap.Insert(target, newDist)
            predecessors[target] = node
        } else if newDist < heap.GetKey(target) {
            heap.DecreaseKey(target, newDist)
            predecessors[target] = node
        }
    }
    
    // 2. Релаксация через метрики ячеек
    for level := LevelID(1); level <= queryLevel; level++ {
        cellID := q.Partition.GetCell(level, node)
        metrics := q.Metrics.Get(level, cellID)
        
        if metrics == nil {
            continue
        }
        
        // Найти индекс node в boundary nodes
        srcIdx := metrics.FindNodeIndex(node)
        if srcIdx == -1 {
            continue  // node не является boundary
        }
        
        // Релаксация до всех остальных boundary nodes
        for dstIdx, dstNode := range metrics.BoundaryNodes {
            if srcIdx == dstIdx {
                continue
            }
            
            path := metrics.Paths[srcIdx][dstIdx]
            if len(path.Edges) == 0 {
                continue  // Нет пути
            }
            
            newDist := dist + path.Distance
            if _, visited := distances[dstNode]; visited {
                continue
            }
            
            if !heap.Contains(dstNode) {
                heap.Insert(dstNode, newDist)
                predecessors[dstNode] = node
            } else if newDist < heap.GetKey(dstNode) {
                heap.DecreaseKey(dstNode, newDist)
                predecessors[dstNode] = node
            }
        }
    }
}
```

#### 5.5. Тестирование корректности (2 дня)
**Сравнение с Dijkstra:**
```go
func TestMLDCorrectness(t *testing.T) {
    // Для 100 случайных пар вершин
    for i := 0; i < 100; i++ {
        source := randomNode()
        target := randomNode()
        
        dijkstraDist, _ := Dijkstra(graph, source, target)
        mldDist, _ := MLDQuery(graph, partition, metrics, source, target)
        
        // Расстояния должны совпадать (с погрешностью)
        assert.InDelta(t, dijkstraDist, mldDist, 0.01)
    }
}
```

#### 5.6. Бенчмарки (2 дня)
```go
func BenchmarkDijkstra(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Dijkstra(graph, source, target)
    }
}

func BenchmarkMLD(b *testing.B) {
    for i := 0; i < b.N; i++ {
        MLDQuery(graph, partition, metrics, source, target)
    }
}
```

**Цель:** MLD минимум в 10x быстрее Dijkstra.

---

## 6. Dynamic Profiles

### Цель
Применять профили маршрутизации во время query без re-customization.

### Задачи

#### 6.1. Интерфейс динамического профиля (2 дня)
```go
// pkg/profile/dynamic.go
type DynamicProfile struct {
    BaseWeights map[EdgeID]float64  // Базовые веса
    Overrides   map[EdgeID]float64  // Временные изменения (аварии)
    mu          sync.RWMutex
}

func (p *DynamicProfile) EdgeWeight(edgeID EdgeID) float64 {
    p.mu.RLock()
    defer p.mu.RUnlock()
    
    // Проверяем override
    if weight, found := p.Overrides[edgeID]; found {
        return weight
    }
    
    // Базовый вес
    return p.BaseWeights[edgeID]
}

func (p *DynamicProfile) SetEdgeWeight(edgeID EdgeID, weight float64) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.Overrides[edgeID] = weight
}

func (p *DynamicProfile) ClearOverride(edgeID EdgeID) {
    p.mu.Lock()
    defer p.mu.Unlock()
    delete(p.Overrides, edgeID)
}
```

#### 6.2. Вычисление весов путей (3 дня)
```go
// pkg/query/profile_query.go
type ProfileMLDQuery struct {
    Graph     *graph.Graph
    Partition *MultiLevelPartition
    Metrics   *MetricsStorage
    Profile   *DynamicProfile  // Динамический профиль
}

func (q *ProfileMLDQuery) evaluatePathWeight(path EdgePath) float64 {
    weight := 0.0
    for _, edgeID := range path.Edges {
        weight += q.Profile.EdgeWeight(edgeID)
    }
    return weight
}
```

#### 6.3. Query с профилем (3 дня)
**Модификация relax():**
```go
func (q *ProfileMLDQuery) relax(...) {
    // 1. Релаксация через рёбра базового графа
    for _, edge := range edges {
        // Используем профиль для веса
        edgeWeight := q.Profile.EdgeWeight(edge.ID)
        newDist := dist + edgeWeight
        // ... стандартная релаксация
    }
    
    // 2. Релаксация через метрики
    for level := LevelID(1); level <= queryLevel; level++ {
        // ...
        path := metrics.Paths[srcIdx][dstIdx]
        
        // Вычисляем вес пути с текущим профилем
        pathWeight := q.evaluatePathWeight(path)
        
        newDist := dist + pathWeight
        // ... стандартная релаксация
    }
}
```

#### 6.4. Кэширование весов (3 дня)
**Оптимизация:**
```go
type CachedProfile struct {
    Profile     *DynamicProfile
    WeightCache sync.Map  // EdgeID -> float64
    Version     int64     // Инкрементируется при изменении
}

func (p *CachedProfile) EdgeWeight(edgeID EdgeID) float64 {
    // Проверяем кэш
    if val, ok := p.WeightCache.Load(edgeID); ok {
        return val.(float64)
    }
    
    // Вычисляем и кэшируем
    weight := p.Profile.EdgeWeight(edgeID)
    p.WeightCache.Store(edgeID, weight)
    return weight
}

func (p *CachedProfile) InvalidateCache() {
    p.WeightCache = sync.Map{}
    atomic.AddInt64(&p.Version, 1)
}
```

#### 6.5. Тестирование (2 дня)
**Проверки:**
- Смена профиля мгновенна (< 1ms)
- Query с профилем корректен
- Изменение веса ребра применяется сразу

**Тест:**
```go
func TestDynamicProfile(t *testing.T) {
    profile := NewDynamicProfile(graph, carProfile)
    
    // Базовый query
    dist1, _ := ProfileMLDQuery(graph, partition, metrics, profile, source, target)
    
    // Меняем вес ребра (авария)
    edgeID := EdgeID(123)
    profile.SetEdgeWeight(edgeID, 9999.0)  // Очень медленно
    
    // Query с новым профилем
    dist2, _ := ProfileMLDQuery(graph, partition, metrics, profile, source, target)
    
    // Расстояние должно измениться
    assert.NotEqual(t, dist1, dist2)
}
```

#### 6.6. Бенчмарки (2 дня)
```go
func BenchmarkMLDWithProfile(b *testing.B) {
    for i := 0; i < b.N; i++ {
        ProfileMLDQuery(graph, partition, metrics, profile, source, target)
    }
}
```

**Цель:** Query с профилем не более чем в 2-3x медленнее базового MLD.

---

## 7. Incremental Updates

### Цель
Обновлять только затронутые части графа при дорожных событиях без полной re-customization.

### Задачи

#### 7.1. Edge Index (уже есть из Customization)
**Проверка:**
```go
// EdgeIndex уже построен в CellMetrics.EdgeIndex
// Для каждого ребра знаем, в каких путях оно используется
```

#### 7.2. Affected Cells Detection (3 дня)
```go
// pkg/update/manager.go
type UpdateManager struct {
    Partition *MultiLevelPartition
    Metrics   *MetricsStorage
}

func (u *UpdateManager) FindAffectedCells(edgeID EdgeID) map[LevelCellKey][]PathRef {
    affected := make(map[LevelCellKey][]PathRef)
    
    // Для каждого уровня
    for level := LevelID(1); level <= LevelID(u.Partition.NumLevels); level++ {
        numCells := u.Partition.NumCells[level]
        
        // Для каждой ячейки
        for cellID := CellID(0); cellID < CellID(numCells); cellID++ {
            metrics := u.Metrics.Get(level, cellID)
            if metrics == nil {
                continue
            }
            
            // Проверяем EdgeIndex
            if pathRefs, found := metrics.EdgeIndex[edgeID]; found {
                key := LevelCellKey{Level: level, CellID: cellID}
                affected[key] = pathRefs
            }
        }
    }
    
    return affected
}
```

#### 7.3. Lazy Update (4 дня)
**Пометка dirty paths:**
```go
type CellMetrics struct {
    // ... существующие поля
    DirtyPaths map[PathKey]bool  // Пути, требующие пересчёта
}

type PathKey struct {
    SourceIdx, DestIdx int
}

func (u *UpdateManager) MarkDirtyPaths(edgeID EdgeID) {
    affected := u.FindAffectedCells(edgeID)
    
    for cellKey, pathRefs := range affected {
        metrics := u.Metrics.Get(cellKey.Level, cellKey.CellID)
        
        for _, ref := range pathRefs {
            key := PathKey{SourceIdx: ref.SourceIdx, DestIdx: ref.DestIdx}
            metrics.DirtyPaths[key] = true
        }
    }
}
```

**Query-time пересчёт:**
```go
func (q *ProfileMLDQuery) evaluatePathWeight(path EdgePath, dirty bool) float64 {
    if dirty {
        // Пересчитать путь с текущими весами
        return q.recomputePathWeight(path)
    }
    
    // Использовать кэшированный вес (если есть)
    if path.CachedWeight > 0 {
        return path.CachedWeight
    }
    
    // Вычислить и закэшировать
    weight := 0.0
    for _, edgeID := range path.Edges {
        weight += q.Profile.EdgeWeight(edgeID)
    }
    path.CachedWeight = weight
    return weight
}
```

#### 7.4. Eager Update (опционально, 7 дней)
**Параллельный пересчёт путей:**
```go
func (u *UpdateManager) UpdateEdgeEager(edgeID EdgeID) {
    affected := u.FindAffectedCells(edgeID)
    
    var wg sync.WaitGroup
    for cellKey, pathRefs := range affected {
        wg.Add(1)
        go func(ck LevelCellKey, refs []PathRef) {
            defer wg.Done()
            u.recomputePaths(ck, refs)
        }(cellKey, pathRefs)
    }
    wg.Wait()
}

func (u *UpdateManager) recomputePaths(cellKey LevelCellKey, pathRefs []PathRef) {
    metrics := u.Metrics.Get(cellKey.Level, cellKey.CellID)
    bounds := metrics.BoundaryNodes
    
    // Пересчитываем только затронутые пути
    for _, ref := range pathRefs {
        source := bounds[ref.SourceIdx]
        dest := bounds[ref.DestIdx]
        
        // Dijkstra от source до dest
        newPath := dijkstraInCell(
            u.Graph,
            source,
            []NodeID{dest},
            cellKey.Level,
            cellKey.CellID,
            u.Partition,
        )[dest]
        
        metrics.Paths[ref.SourceIdx][ref.DestIdx] = newPath
    }
}
```

#### 7.5. Event API (3 дня)
```go
// pkg/update/events.go
type TrafficEvent struct {
    EdgeID    EdgeID
    NewSpeed  float64     // км/ч
    Duration  time.Duration
    Timestamp time.Time
}

type EventManager struct {
    UpdateManager *UpdateManager
    Profile       *DynamicProfile
    ActiveEvents  map[string]*TrafficEvent  // eventID -> event
    mu            sync.RWMutex
}

func (em *EventManager) ApplyEvent(eventID string, event TrafficEvent) {
    em.mu.Lock()
    defer em.mu.Unlock()
    
    // Сохраняем событие
    em.ActiveEvents[eventID] = &event
    
    // Обновляем профиль
    edge := em.UpdateManager.Graph.GetEdge(event.EdgeID)
    newWeight := edge.Distance / (event.NewSpeed / 3.6)
    em.Profile.SetEdgeWeight(event.EdgeID, newWeight)
    
    // Помечаем затронутые пути (lazy) или пересчитываем (eager)
    em.UpdateManager.MarkDirtyPaths(event.EdgeID)
    
    // Планируем автоматическое удаление события
    time.AfterFunc(event.Duration, func() {
        em.ExpireEvent(eventID)
    })
}

func (em *EventManager) ExpireEvent(eventID string) {
    em.mu.Lock()
    defer em.mu.Unlock()
    
    event, found := em.ActiveEvents[eventID]
    if !found {
        return
    }
    
    // Восстанавливаем базовый вес
    em.Profile.ClearOverride(event.EdgeID)
    
    // Очищаем dirty flags
    em.UpdateManager.ClearDirtyPaths(event.EdgeID)
    
    delete(em.ActiveEvents, eventID)
}
```

#### 7.6. Spatial Index (опционально, 5 дней)
**Для быстрого поиска событий по координатам:**
```go
// pkg/spatial/rtree.go
type SpatialIndex struct {
    tree rtree.RTree  // github.com/dhconnelly/rtree
}

func (si *SpatialIndex) FindCellsInBBox(minLat, minLon, maxLat, maxLon float64) []LevelCellKey {
    // Найти все ячейки, пересекающиеся с bounding box
}
```

#### 7.7. Тестирование (3 дня)
**Сценарии:**
```go
func TestIncrementalUpdate(t *testing.T) {
    // 1. Базовый query
    dist1, path1 := Query(source, target)
    
    // 2. Применяем событие (авария на ребре в path1)
    edgeID := path1[5]  // Ребро в середине пути
    eventMgr.ApplyEvent("accident1", TrafficEvent{
        EdgeID:   edgeID,
        NewSpeed: 10.0,  // Очень медленно
        Duration: 1 * time.Hour,
    })
    
    // 3. Query после события
    dist2, path2 := Query(source, target)
    
    // 4. Проверки
    assert.Greater(t, dist2, dist1)  // Путь стал длиннее
    assert.NotEqual(t, path1, path2)  // Путь изменился
    
    // 5. Событие истекло
    eventMgr.ExpireEvent("accident1")
    
    // 6. Query после истечения
    dist3, path3 := Query(source, target)
    
    assert.Equal(t, dist1, dist3)  // Вернулись к исходному
}
```

#### 7.8. Бенчмарки (2 дня)
```go
func BenchmarkUpdateEvent(b *testing.B) {
    for i := 0; i < b.N; i++ {
        eventMgr.ApplyEvent("test", event)
    }
}

func BenchmarkQueryAfterUpdate(b *testing.B) {
    eventMgr.ApplyEvent("accident", event)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Query(source, target)
    }
}
```

**Цель:**
- ApplyEvent < 10ms (lazy) или < 100ms (eager)
- Query после update не более чем в 1.5x медленнее

---

## 📊 Итоговые метрики успеха

### Производительность
- **Query время:** < 5ms для средних дистанций (50km)
- **Ускорение vs Dijkstra:** минимум 10x
- **Смена профиля:** < 1ms
- **Применение события:** < 10ms (lazy) или < 100ms (eager)

### Качество
- **Корректность:** пути совпадают с Dijkstra (100% тестов)
- **Boundary nodes:** < 20% от общего числа вершин
- **Размер метрик:** < 100MB для Monaco, < 10GB для крупного региона

### Функциональность
- ✅ Динамические профили (car, bike, walk)
- ✅ Дорожные события (аварии, пробки)
- ✅ Инкрементальные обновления без re-customization
- ✅ Параллельная обработка запросов

---

## 🛠️ Технологический стек

### Язык и библиотеки
- **Go:** 1.21+
- **OSM Parser:** `github.com/paulmach/osm`
- **Heap:** Custom implementation или `container/heap`
- **Spatial Index:** `github.com/dhconnelly/rtree` (опционально)
- **Compression:** `compress/gzip`
- **Serialization:** Binary (custom) или Protocol Buffers

### Инструменты
- **Визуализация:** GeoJSON + QGIS/geojson.io
- **Тестирование:** Go testing + benchmarks
- **Профилирование:** pprof

### Структура проекта
```
mld/
├── cmd/
│   ├── osm-import/      # Импорт OSM данных
│   ├── partition/       # Partitioning утилита
│   ├── customize/       # Customization утилита
│   ├── query-server/    # HTTP сервер для query
│   └── event-manager/   # Управление дорожными событиями
├── pkg/
│   ├── graph/           # Базовые структуры графа
│   ├── osm/             # OSM парсинг
│   ├── partition/       # Partitioning алгоритмы
│   ├── customize/       # Customization
│   ├── query/           # Query алгоритмы
│   ├── profile/         # Профили маршрутизации
│   ├── update/          # Инкрементальные обновления
│   ├── storage/         # Сериализация/десериализация
│   └── spatial/         # Spatial index (опционально)
├── internal/
│   └── util/            # Вспомогательные функции
├── test/
│   └── data/            # Тестовые данные (Monaco)
├── profiles/            # YAML конфигурации профилей
├── data/                # Обработанные данные (gitignore)
└── PLAN.md              # Этот файл
```

---

## 📅 Примерные сроки

| Этап | Длительность | Накопительно |
|------|--------------|--------------|
| 1. OSM Loading | 1.5 недели | 1.5 недели |
| 2. Profile Interface | 1.5 недели | 3 недели |
| 3. Partitioning | 1.5 недели | 4.5 недели |
| 4. Customization | 2 недели | 6.5 недель |
| 5. Query | 2 недели | 8.5 недель |
| 6. Dynamic Profiles | 2 недели | 10.5 недель |
| 7. Incremental Updates | 3-4 недели | 13.5-14.5 недель |

**Итого:** ~3.5 месяца для полной реализации.

**Минимальный PoC (этапы 1-5):** ~2 месяца.

---

## 🎯 Следующие шаги

1. **Создать структуру проекта:**
```bash
cd E:\_projects\work\mld
mkdir -p cmd/{osm-import,partition,customize,query-server}
mkdir -p pkg/{graph,osm,partition,customize,query,profile,storage}
mkdir -p test/data
mkdir -p profiles
go mod init github.com/yourusername/mld
```

2. **Скачать Monaco OSM:**
```bash
cd test/data
wget https://download.geofabrik.de/europe/monaco-latest.osm.pbf
```

3. **Начать с этапа 1: OSM Loading**
