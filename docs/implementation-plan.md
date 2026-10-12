# PLC Reader 設計・実装計画

## 1. この文書の位置づけ

本書は `plc-reader-prototype.md` を具体的な実装へ落とし込むためのレビュー用計画である。
まだ確定していないPLCプロトコルやOTLP上の表現は勝手に固定せず、推奨案、代替案、決定に必要な情報を分けて記載する。

最初の実装では、次の性質を優先する。

- Goの標準的で小さな構造にする。
- 通信、データ処理、スケジューリング、出力を分離する。
- interfaceは差し替えが必要な境界に置き、実装のためだけの抽象化は増やさない。
- 各コンポーネントを「契約を定義 → テストを追加 → 最小実装 → 統合確認」の順で完成させる。
- エラーをPLC単位・RegionTask単位に閉じ込める。
- キューや再試行は必ず有限にし、無制限なメモリ消費を許さない。

初期スコープ外は、設定のホットリロード、分散スケジューリング、永続再送キュー、汎用PLCアドレスエンジン、高度な接続プールである。

## 2. 推奨アーキテクチャ

```text
YAML files
    |
    v
config.Loader/Compiler ---- static validation
    |
    v
app.Builder
    |-- shared PLC clients (one per PLC)
    |-- RegionProcessor + initial state (one per PLC/region)
    |-- RegionTask (one per PLC/region)
    |-- OutputPipeline -> OTLP Exporter
    `-- Scheduler
             |
             v
        RegionTask.Run
             |-- WordReader.ReadWords
             |-- RegionProcessor.Process
             `-- Sink.Submit
```

重要な境界は次のとおり。

1. `RegionProcessor` はネットワークを知らず、ワード列を測定値へ変換する。
2. `RegionTask` は読取、変換、出力受付を一回分の処理として調停する。
3. `Scheduler` は時刻と重複実行だけを扱い、PLCやセンサーを知らない。
4. PLC実装とOTLP実装は、それぞれ小さなinterfaceのアダプターにする。
5. `Builder` は起動時に参照解決と依存関係の組み立てを完了し、周期処理中に設定を再解析しない。

## 3. Goパッケージ構成案

```text
cmd/plc-reader/             CLI、signal処理、終了コード
internal/app/               Builder、起動・終了のライフサイクル
internal/config/            YAML読込、構文検証、参照解決、CompiledConfig
internal/domain/            Snapshot、Measurement、値型、ID、アドレス
internal/process/           ワードdecode、analog/digital処理
internal/task/              RegionTaskと、その利用側interface
internal/scheduler/         interval別の起動、重複実行防止、停止
internal/plc/               PLCプロトコル、TCP client、接続管理
internal/output/            bounded queue、batch、retry、flush
internal/output/otlp/       OTLP変換と送信アダプター
internal/testutil/plcsim/   TCP PLC simulator
testdata/                   設定、PLC応答、OTLP期待値
```

依存方向は概ね次に限定する。

```text
cmd -> app -> config
           -> scheduler -> task -> domain
                                -> consumer-owned interfaces
           -> plc -------------> domain
           -> process ---------> domain
           -> output/otlp -----> domain
```

`domain` は他の `internal` パッケージをimportしない。`plc`、`process`、`otlp` 同士も直接依存しない。
interfaceは原則として利用側に定義する。例えば `task.WordReader` を `plc.Client` が暗黙に満たす形にし、実装側に巨大な共通interfaceを置かない。

## 4. 主要な型とinterfaceの案

以下は形をレビューするための案であり、名前や細部は各実装段階のテスト作成時に確定する。

### 4.1 ドメイン型

```go
type PLCID string
type RegionID string
type SensorID string

type Address struct {
    Area   string // 初期候補: DM。対応値はPLCプロトコル確定時に絞る。
    Offset uint32 // 16bit word単位
}

type RegionSnapshot struct {
    PLCID       PLCID
    RegionID    RegionID
    CollectedAt time.Time
    Words       []uint16
}

type ValueKind uint8

const (
    ValueBool ValueKind = iota + 1
    ValueInt64
    ValueFloat64
)

type Value struct {
    Kind    ValueKind
    Bool    bool
    Int64   int64
    Float64 float64
}

type Measurement struct {
    PLCID      PLCID
    RegionID   RegionID
    SensorID   SensorID
    ObservedAt time.Time
    Value      Value
    Unit       string
}
```

`uint16` と `uint32` は `int64` へ正確に格納できる。スケーリング後だけ `float64` を使う。
`any` や `map[string]any` をドメイン値に使用せず、変換失敗をコンパイル時または明示的エラーとして扱う。

### 4.2 データ処理

```go
type ProcessorState struct {
    // センサーごとの前回観測値と最終受付時刻。
    // 外部から直接変更できない形を最終実装で選ぶ。
}

type ProcessResult struct {
    Measurements []domain.Measurement
    NextState    ProcessorState
}

type RegionProcessor interface {
    Process(
        snapshot domain.RegionSnapshot,
        previous ProcessorState,
    ) (ProcessResult, error)
}
```

処理結果と次状態を同時に返し、`RegionTask` は出力パイプラインがbatchを受理した場合だけ `NextState` を確定する。
この方式なら、キュー受付失敗時にデジタル変化を次周期で再提示でき、Processor単体も決定的にテストできる。

ProcessorはRegion型ごとに別実装へ分裂させすぎず、起動時にcompile済みのsensor decoder一覧を持つ一つの実装を第一候補とする。

### 4.3 RegionTaskが利用する境界

```go
type WordReader interface {
    ReadWords(
        ctx context.Context,
        address domain.Address,
        count uint16,
    ) ([]uint16, error)
}

type Processor interface {
    Process(
        snapshot domain.RegionSnapshot,
        previous process.ProcessorState,
    ) (process.ProcessResult, error)
}

type Sink interface {
    Submit(ctx context.Context, batch []domain.Measurement) error
}

type RegionTask interface {
    ID() string
    Interval() time.Duration
    Run(ctx context.Context) error
}
```

`ReadWords` は要求したword数と完全一致しない応答を成功扱いしない。
TaskはPLC client、compile済みProcessor、Sink、現在状態、および最大1件の未受付 `ProcessResult` を保持する。
Taskの実行は同期的にし、goroutineの生成責務はSchedulerに置く。

測定値が0件の場合はSinkを呼ばず、Processorの `NextState` は正常な読取結果として確定する。
測定値がある場合は `Submit` 成功後にだけ確定する。
`Submit` が失敗した場合はresultをTask内に保持し、次回実行では新しいPLC読取より先に同じbatchを再提出する。
これにより観測済みのデジタル変化を後続snapshotで上書きしない。未受付batchがある間は新しい値を収集しないため、長い出力停止中にもサンプリングを継続する要件があるなら、永続キューを含む別設計が必要になる。

### 4.4 Scheduler境界

```go
type Task interface {
    ID() string
    Interval() time.Duration
    Run(ctx context.Context) error
}

type Clock interface {
    NewTicker(interval time.Duration) Ticker
}

type Ticker interface {
    C() <-chan time.Time
    Stop()
}
```

Clock interfaceは実時間待ちをしないテストのためにだけ導入する。
Schedulerは同じintervalのTaskを同じtickerへ登録するが、Task自体はPLC/Regionごとに独立させる。

### 4.5 出力境界

```go
type Exporter interface {
    Export(ctx context.Context, batch []domain.Measurement) error
    Shutdown(ctx context.Context) error
}
```

`output.Pipeline` はTask向けの `Sink` を実装し、内部workerから `Exporter` を呼ぶ。
`Submit` 成功は「Collectorへ到達した」ではなく「有限キューがbatchの所有権を受け取った」を意味する。
この違いはメトリクスとログで明示する。

## 5. 設定ファイル案

初期実装ではinclude機能を作らず、単一YAMLを推奨する。ただし複数PLCで共有できるよう、interface layoutには名前を付ける。

```yaml
schema_version: 1

tcp:
  connect_timeout: 2s
  request_timeout: 2s

interfaces:
  - id: common-v1
    version: 1
    base_address:
      area: DM
      offset: 20000
    uint32_word_order: high_low # 要確認
    regions:
      - id: management
        offset: 0
        words: 10
        type: uint16
        interval: 60s
      - id: digital
        offset: 10
        words: 10
        type: bitfield
        interval: 4s
      - id: analog_u16
        offset: 100
        words: 200
        type: uint16
        interval: 60s

plcs:
  - id: plc01
    host: 192.168.1.10
    port: 8500
    interface: common-v1

sensors:
  - id: motor_running
    plc: plc01
    region: digital
    bit_offset: 37
    mode: on_change
    heartbeat: 24h
  - id: temperature_in_house
    plc: plc01
    region: analog_u16
    word_offset: 12
    scale: 0.1
    unit: Cel
    mode: always

output:
  protocol: grpc
  endpoint: localhost:4317
  insecure: true
  timeout: 2s
  queue_capacity: 1024
  batch_size: 100
  flush_interval: 1s
```

設定は二段階で扱う。

1. `Load`: YAMLをraw structへ読み、未知フィールドを拒否する。
2. `Compile`: ID参照、offset、型、重複、範囲を検証し、実行用 `CompiledConfig` へ変換する。

最低限の静的検証は次のとおり。

- `schema_version` が対応値である。
- IDが空でなく、種類ごとに一意である。
- PLCのhost、port、interface参照が有効である。
- timeout、interval、heartbeat、queue/batch値が正である。
- Regionが共有I/Fの範囲内にあり、意図しない重複がない。
- SensorのPLC/Region参照が存在する。
- bitfieldは `bit_offset < words * 16` である。
- 16bitは `word_offset < words` である。
- 32bitは2wordを超えず、word orderが指定されている。
- `on_change` はdigital、`always` はanalogという初期制約を満たす。
- scaleがNaN/Infでなく、未指定時は1.0である。
- TLS/insecure設定が矛盾せず、endpointが有効である。

動的検証として、各PLCの管理RegionからI/F versionを読み、設定値と照合する。
不一致時に全体を起動失敗とするか、そのPLCだけ隔離するかは実装前に決める。

## 6. 実行シーケンス

### 6.1 初期化

```text
main
  -> config.Load
  -> config.Compile
  -> app.Build
       -> PLCごとにclientを1個生成
       -> PLC/RegionごとにProcessorとRegionTaskを生成
       -> OutputPipelineを生成
       -> SchedulerへTaskを登録
  -> PLC I/F version probe
  -> OutputPipeline.Start
  -> Scheduler.Run
```

設定エラーはネットワークへ接続する前に全件をまとめて返す。
Builderは同一PLCを参照するTaskへ同じclientを渡し、通信直列化の単位を明確にする。

### 6.2 周期実行

```text
Ticker
  -> Scheduler checks task is idle
  -> RegionTask.Run(ctx with deadline)
       -> retry a pending batch first; return if it was pending
       -> PLCClient.ReadWords
       -> build RegionSnapshot with collection time
       -> Processor.Process(snapshot, currentState)
       -> Sink.Submit(measurements)
       -> commit NextState only after Submit succeeds
  -> mark task idle
```

最初の収集を起動直後に実行するか、最初のinterval満了後に実行するかは設定可能にせず、一つに決める。
推奨は起動直後である。設定ミスと接続不良を早く検出できる。

### 6.3 エラー

- PLC timeout/通信エラー: 測定値とProcessor状態を更新しない。clientは破損接続を閉じ、次周期に再接続する。
- Processorエラー: batchを出力せず、状態を更新しない。設定で防げなかったデータ不整合として記録する。
- Sink受付エラー: 状態を更新せず、未受付resultをTaskに保持する。次周期は新規読取より先に同じbatchを再提出する。
- Exporterエラー: Pipeline内で有限回または有限時間retryし、queue使用率と失敗を自己監視へ出す。
- あるPLC/Taskの失敗: 他PLC/Taskを停止しない。
- panic: 通常のエラー制御には使わない。goroutine境界のrecoverを導入するなら、障害を記録してTaskを次周期に再実行可能な状態へ戻す。

### 6.4 終了

```text
SIGTERM/SIGINT
  -> cancel Scheduler
  -> 新しいTask起動を停止
  -> 実行中Taskをshutdown timeout内で待機
  -> OutputPipelineをflush
  -> Exporter.Shutdown
  -> PLC clientsをClose
  -> main returns exit code
```

終了処理には全体deadlineを設ける。期限切れでも永久に待たない。

## 7. 並行実行と接続管理

### 7.1 Scheduler

- intervalごとにtickerを一つ持つ。
- tick時に、そのintervalの各Taskを独立goroutineで起動する。
- Taskごとに実行中フラグまたは容量1のsemaphoreを持つ。
- 前回実行中なら新しい実行をqueueせず、`skipped` として記録する。
- 終了開始後は新規起動しない。
- `sync.WaitGroup` で実行中Taskを待つ。

実時間テストは避け、fake Clockでtickを明示的に進める。race detectorで重複実行防止を検証する。

### 7.2 PLCClient

初期推奨はPLCごとにclient instanceを一つ共有する方式である。

- client内のmutexで、同一接続上のrequest/responseを直列化する。
- 異なるPLCは並行に読める。
- 初回要求時にlazy connectし、正常な接続は再利用する。
- connect/read/writeにcontext由来のdeadlineを設定する。
- timeout、EOF、frame不正、protocol error時は接続を無効化して閉じる。
- 同じ周期内では自動再試行せず、次周期に再接続する。二重応答や処理時間の肥大化を避けるためである。
- PLCの最大read word数を超えるRegionは、client adapter内で複数要求へ分割し、完全な結果だけを返す。

具体的なframe、エラーコード、最大word数、request IDの有無はプロトコル仕様確定後に決める。

## 8. OTLP出力方式の比較

| 方式 | 長所 | 注意点 | 適性 |
| --- | --- | --- | --- |
| OTel Metrics SDK | 標準的なresource、exporter、retry、自己監視と統合しやすい | 通常のinstrument APIは個々の過去測定点へ任意timestampを付ける用途に向かない。デジタル変化履歴の表現を要検証 | Reader自身のメトリクスには最適 |
| OTLP Metrics protobufを直接生成 | 各DataPointのtimestampと値を明示できる。内部Measurementとの対応が単純 | resource/scope/metric metadata、batch、retry、接続管理を自分で担う。OTLP metric semanticsの確認が必要 | PLC測定値の第一候補 |
| AnalogをMetrics、Digital変化をOTLP Logs | gaugeとeventの意味が自然で、デジタル履歴を時刻付きeventにできる | 後段が2 signalを扱う必要があり、利用側queryが複雑になる | 後段がLogsを許容する場合の有力案 |
| Collectorの`pdata`を利用 | Collectorとの構造互換性が高い | PLC Readerには依存が大きくなりやすい | 初期プロトタイプでは非推奨 |

暫定推奨は次のとおり。

- Reader自身の稼働状況にはOpenTelemetry Go SDKを使う。
- PLC測定値は内部 `Measurement` からOTLP protobufへ直接変換するadapterを作る。
- DigitalをMetricとして送るかLog eventとして送るかは、後段の期待schemaを確認してADRで確定する。
- 依存候補は公式の `go.opentelemetry.io/proto/otlp` とgRPC/protobufに限定し、Collector本体への依存は避ける。
- retryは無限にせず、Pipelineの有限queueとCollector側の機能に責務を分ける。

Digital変化を絶対に失わない要件が、プロセス停止や長時間のCollector停止にも及ぶなら、初期スコープ外としている永続キューが必要になる。
メモリ上の有限queueだけで保証できるのは、その容量とプロセス生存期間の範囲内である。

## 9. テスト戦略

### 9.1 単体テスト

- table-driven testを基本にする。
- decoderは境界値、signed変換、uint32 word order、bit 0/15/16を網羅する。
- digital処理は初回、変化なし、変化あり、24時間heartbeat、時刻境界、出力受付失敗後の再提示を確認する。
- configは正常例に加え、未知フィールド、重複ID、参照切れ、範囲外offset、型不一致を確認する。
- RegionTaskはfake reader/processor/sinkで呼出順、時刻、エラー時の状態非更新、未受付batchの優先再提出を確認する。
- Schedulerはfake clockとblocking taskで重複skip、異なるTaskの並列、cancel、waitを確認する。
- PLC protocol codecはgolden byte列でencode/decodeを確認する。
- OTLP変換はprotobufをdecodeし、timestamp、attributes、value、unitを構造比較する。

### 9.2 PLC simulator

`internal/testutil/plcsim` にテスト専用TCP serverを置く。

機能は段階的に追加する。

1. 指定アドレスへ固定word列を返す。
2. 読取履歴を記録し、request frameをassertできる。
3. 応答遅延、切断、partial frame、protocol errorを注入できる。
4. 接続し直した後に正常復帰できる。
5. 複数clientを分離して扱える。

実PLCをCIの前提にしない。実機確認は別の手動acceptance testとし、通常の `go test ./...` からは分離する。

### 9.3 統合・E2E

- simulator 2台相当を起動し、片方の障害が他方へ波及しないことを確認する。
- fake OTLP gRPC serverでexport requestを受け、設定から測定値までを通して検証する。
- Digital 4秒、Analog 60秒の実時間待ちはせず、短いテスト用intervalまたはfake clockを使う。
- `go test -race ./...` でTask、client、Pipelineの競合を検査する。
- `go vet ./...` と単一バイナリbuildを行う。
- shutdown時にgoroutine、listener、connectionが残らないことを確認する。

## 10. 実装順序

各段階で必ず次の小サイクルを繰り返す。

1. 利用側から見た型・interface・エラー分類を規定する。
2. 正常、境界、失敗を表すテストを先に書き、意図した理由で失敗することを確認する。
3. テストを通す最小実装を書く。
4. `go test`、必要なら `-race` を実行する。
5. 上位コンポーネントが使える公開契約と未決事項をレビューする。

### Phase 0: module、品質ゲート、ADRひな型

成果物:

- `go.mod`、最小の `cmd/plc-reader`
- Makefileまたは単純なdocumented commands
- `go test ./...`、`go test -race ./...`、`go vet ./...`、buildの基本経路
- 重要判断を残す `docs/adr/`

完了条件:

- 空実装でも全コマンドが再現可能である。
- CIとローカルが同じコマンドを使う。
- PLC仕様やOTLP schemaを仮実装で固定していない。

### Phase 1: domain型とRegionSnapshot decoder

先に規定する契約:

- ID、Address、RegionSnapshot、Measurement、Value
- bit、uint16、int16、uint32 decoder

先に書くテスト:

- 最小/最大値、符号境界、offset境界、word不足、word order

完了条件:

- ネットワークや設定に依存せず、word列から全対象型を決定的に読める。

### Phase 2: RegionProcessor

先に規定する契約:

- compile済みsensor定義
- `Process(snapshot, previousState) -> result + nextState`
- Analog always、Digital on-change/heartbeatの規則

先に書くテスト:

- 初回値、変化、無変化、heartbeat、scale、複数sensor、失敗時の状態

完了条件:

- U16/S16/U32とDigitalを処理できる。
- PLC通信やOTLPを使わずに振る舞いが完全にテストされる。

### Phase 3: config Loader/Compiler

先に規定する契約:

- raw schema、`Load`、`Compile`、validation error一覧
- named interface layoutとPLC/sensor参照

先に書くテスト:

- `testdata` の正常設定と、検証項目ごとの異常設定

完了条件:

- 未知フィールドを拒否する。
- 起動後に再解決不要なruntime specを生成する。
- 一度の実行で複数の設定エラーを読みやすく報告する。

### Phase 4: RegionTask（fake依存による最初の縦切り）

先に規定する契約:

- `WordReader`、`Processor`、`Sink`、`RegionTask`
- 状態commitとエラー分類

先に書くテスト:

- Read -> Process -> Submitの順序
- read/process/submit失敗、0件、context cancel、state commit、未受付batch中に再読取しないこと

完了条件:

- fake readerから得たwordがfake sinkへ測定値として届く。
- 失敗時に誤った測定値や状態更新がない。

### Phase 5: Scheduler

先に規定する契約:

- Task、Clock/Ticker、skip規則、shutdown規則

先に書くテスト:

- 同一Taskの重複なし、異なるTaskの並行、interval grouping、cancel、panic時のrunning解除

完了条件:

- race testが通る。
- 遅いTaskのtickを蓄積せず、skip回数を観測できる。

### Phase 6: PLC protocolとTCP client

開始条件:

- 使用プロトコル、frame、address表現、word order、最大読取長が確定している。

先に規定する契約:

- protocol codec、`ReadWords`、接続再利用/無効化、error分類

先に書くテスト:

- golden frame、simulator正常系、timeout、partial response、切断、再接続、同一PLC直列化

完了条件:

- simulatorに対する読取と障害復旧が通る。
- 複数Taskが同じclientを共有してもframeが混在しない。

### Phase 7: OutputPipelineとOTLP adapter

開始条件:

- Digital/AnalogをOTLP MetricsまたはLogsのどちらで表現するか決まっている。

先に規定する契約:

- Sinkの受付保証、queue full規則、batch/retry/flush
- OTLP resource、scope、metric/log名、attributes、timestamp、TLS

先に書くテスト:

- 順序、batch境界、queue full、retry、shutdown flush、fake collectorのrequest内容

完了条件:

- 任意の測定時刻を保持してfake collectorへ送れる。
- Collector停止時にもメモリが無制限に増えない。
- 受付済みbatchの扱いとdrop条件が観測可能である。

### Phase 8: Builder、I/F version確認、app lifecycle、CLI

先に規定する契約:

- `Build(CompiledConfig)`、`Run(ctx)`、`Shutdown(ctx)`
- 起動失敗と部分障害の扱い、終了コード

先に書くテスト:

- 参照されたclientの共有、Task数、version一致/不一致、signal相当cancel、終了順序

完了条件:

- 一つの設定から全runtime componentが構築される。
- 正常終了と設定/起動エラーで適切な終了コードを返す。

### Phase 9: E2E、運用性、単一バイナリ

成果物:

- 2 PLC simulator + fake collectorのE2E
- structured logging
- Reader自身のメトリクス
- example config、運用README、build/version情報

最低限の自己監視候補:

- read成功/失敗/latency
- task実行/skip/latency
- processed/submitted/exported/dropped measurement数
- output queue長、export失敗
- reconnect数、I/F version mismatch

完了条件:

- 一方のPLC障害中も他方の測定が出力される。
- Digital変化とAnalog周期値が期待timestampで届く。
- graceful shutdownで受付済みbatchを期限内にflushする。
- `go test -race ./...`、`go vet ./...`、buildが成功する。

## 11. 実装前に決定が必要な事項

### PLC側

1. 使用する具体的なPLCプロトコルと仕様書の版。
2. request/response frame、device area/address形式、最大読取word数。
3. 16bit byte order、32bit word order、signed表現。
4. request IDやresponse照合方法、PLCエラーコード。
5. 接続のidle timeout、PLCが許容する同時接続数。

### 測定値と出力

6. 後段が期待するOTLP signal（Metrics、Logs、または両方）。
7. metric/log名、resource/point attributes、unit、PLC/Sensor IDの命名規則。
8. Digital初回値を必ず出力するか。推奨は「出力する」。
9. 24時間heartbeatを最終出力時刻基準にするか。推奨は「最後にPipelineへ受付された時刻」。
10. Collector長時間停止時の許容drop、queue容量、必要なら永続化を追加する条件。
11. OTLPのTLS、認証、圧縮要件。

### 起動と障害方針

12. I/F version不一致時に全体を起動失敗にするか、該当PLCだけ隔離するか。
13. 最初の収集を即時実行するか。推奨は「即時」。
14. Region間の読取整合性が必要か。各Regionを別時刻に読んでよいか。
15. 同一PLCのRegion読取順に要件があるか。

## 12. 推奨する最初のレビュー境界

実装開始前に、少なくとも次をレビューして合意する。

1. パッケージと依存方向。
2. 単一YAML + named interface layoutという設定方針。
3. Processorの状態をTaskがcommitする方式。
4. PLCごとに一つのclientを共有し、同一PLC通信を直列化する方式。
5. Schedulerが遅延tickを蓄積せずskipする方式。
6. OTLP signal/schemaと、有限queueを超えた場合の保証範囲。
7. 上記「実装前に決定が必要な事項」のうちPhase 1〜5に影響する項目。

PLC protocolと最終OTLP schemaが未確定でも、Phase 0〜5はfake境界を使って進められる。
ただし、仮のwire protocolや仮のtelemetry schemaを本実装として固定しない。
