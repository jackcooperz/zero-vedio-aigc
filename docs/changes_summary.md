# 场景任务与视频合成修复总结

## 概述

本文档记录了在实现多关键帧图片生成和视频合成功能过程中发现的问题及修复方案。

---

## 改动点列表

### 1. 前端 JavaScript 修复

#### 1.1 `renderSceneList` 函数 - 任务过滤空值检查

**文件**: `cmd/story-video-server/main.go` (内嵌 HTML/JS)

**问题**: 当任务的 `scene_id` 字段为 `undefined` 时，调用 `startsWith` 方法导致 JavaScript 报错 "Cannot read properties of undefined (reading 'startsWith')"，中断整个渲染流程，场景任务列表显示为空。

**修复**: 在过滤任务时添加空值检查：

```javascript
const sceneTasks = (tasks || []).filter(function(task) {
  if (!task.scene_id) return false;
  return task.scene_id === scene.scene_id || task.scene_id.startsWith(scene.scene_id + "_kf");
});
```

#### 1.2 `canGenerateFinalVideo` 函数 - 关键帧任务检查

**文件**: `cmd/story-video-server/main.go` (内嵌 HTML/JS)

**问题**: 原函数只检查单个图片任务状态，未考虑多关键帧场景需要所有关键帧图片任务都完成。

**修复**: 更新逻辑以检查所有关键帧图片任务：

```javascript
const sceneTasks = (tasks || []).filter(function(task) {
  if (!task.scene_id) return false;
  return task.scene_id === scene.scene_id || task.scene_id.startsWith(scene.scene_id + "_kf");
});
const imageTasks = sceneTasks.filter(function(task) { return task.kind === "scene_image_generation"; });
const allImagesDone = imageTasks.length > 0 && imageTasks.every(function(task) { return task.status === "success"; });
```

#### 1.3 `renderSceneList` 函数 - 恢复 `imageTasks` 变量定义

**文件**: `cmd/story-video-server/main.go` (内嵌 HTML/JS)

**问题**: 在修改 `canRunVideo` 逻辑时误删了 `imageTasks` 变量定义，导致后续代码引用未定义变量，场景任务列表渲染失败。

**修复**: 恢复变量定义：

```javascript
const canRunVideo = scene.image_status === "success" && scene.audio_status === "success";

const imageTasks = sceneTasks.filter(function(task) { return task.kind === "scene_image_generation"; });
const imageButtonsHtml = imageTasks.map(function(task) { ... });
```

---

### 2. 后端 Go 修复

#### 2.1 关键帧图片生成成功后更新 `image_status`

**文件**: `cmd/story-video-server/main.go`

**函数**: `generateSceneImage` (约第 1330-1348 行)

**问题**: 当关键帧图片生成成功时，代码只更新了 `scene.Status = "image_ready"`，但**没有更新** `scene.ImageStatus`。`image_status = "success"` 只在普通场景任务（非关键帧）成功时才设置。导致前端判断 `scene.image_status === "success"` 时始终为 false，"运行视频任务"按钮无法点击。

**修复**: 在关键帧任务成功后，检查是否所有关键帧都已完成，如果是则更新 `image_status`：

```go
if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
    // 关键帧任务成功
    scene.Keyframes[keyframeIndex].ImageLocalPath = localPath
    // ... 其他字段更新 ...
    scene.Status = "image_ready"
    scene.UpdatedAt = finishedAt
    // 检查是否所有关键帧都已完成
    allKeyframesDone := true
    for _, kf := range scene.Keyframes {
        if kf.ImageLocalPath == "" {
            allKeyframesDone = false
            break
        }
    }
    if allKeyframesDone {
        scene.ImageStatus = "success"
    }
}
```

#### 2.2 视频合成时检查关键帧图片路径

**文件**: `cmd/story-video-server/main.go`

**函数**: `composeSceneVideo` (约第 1515-1528 行)

**问题**: `composeSceneVideo` 函数在检查图片是否就绪时，只检查了 `scene.ImageLocalPath`，但对于有关键帧的场景，图片路径存储在 `scene.Keyframes[i].ImageLocalPath` 中，导致检查失败并返回 "scene image is not ready" 错误。

**修复**: 支持关键帧和普通场景两种情况：

```go
// 检查图片是否就绪（支持关键帧和普通场景）
if len(scene.Keyframes) > 0 {
    for _, kf := range scene.Keyframes {
        if kf.ImageLocalPath == "" || !fileExists(kf.ImageLocalPath) {
            return sceneFile{}, errors.New("scene keyframe image is not ready")
        }
    }
} else {
    if scene.ImageLocalPath == "" || !fileExists(scene.ImageLocalPath) {
        return sceneFile{}, errors.New("scene image is not ready")
    }
}
```

---

### 3. 数据修复

#### 3.1 手动更新场景文件的 `image_status`

**文件**: 
- `projects/pv_1777268282738/scenes/s01.json`
- `projects/pv_1777268282738/scenes/index.json`

**问题**: 之前的关键帧图片生成没有触发 `image_status` 更新，导致两个文件中的 `image_status` 仍为 `"pending"`。

**修复**: 手动将 `image_status` 从 `"pending"` 改为 `"success"`。

**注意**: 系统有两份场景数据——单个场景文件（如 `s01.json`）和索引文件（`index.json`）。`writeSceneFile` 函数会同时写入这两个文件，但手动修改单个文件不会触发同步。

---

## 问题根因分析

### 数据同步机制

系统使用双份场景数据存储：
1. **单个场景文件**: `projects/{projectID}/scenes/{sceneID}.json`
2. **索引文件**: `projects/{projectID}/scenes/index.json`

`writeSceneFile` 函数在写入单个场景文件后，会读取 `index.json`，更新对应场景数据，然后写回 `index.json`。这保证了数据一致性。

### 关键帧图片生成的数据流

1. 用户触发关键帧图片生成任务
2. `generateSceneImage` 函数读取场景文件
3. 生成图片并保存到 `scene.Keyframes[i].ImageLocalPath`
4. 调用 `writeSceneFile` 写入场景文件
5. `writeSceneFile` 同步更新 `index.json`

**问题**: 之前的代码在第 4 步后没有设置 `scene.ImageStatus = "success"`，导致即使所有关键帧都生成完成，`image_status` 仍然是 `"pending"`。

---

## 修复后的行为

1. **场景任务列表正常显示**: 前端 JavaScript 不再因空值报错而中断渲染
2. **"运行视频任务"按钮正确启用**: 当所有关键帧图片生成完成且音频任务完成后，按钮自动启用
3. **视频合成正常工作**: 后端正确检查关键帧图片路径，不再返回 "scene image is not ready" 错误
4. **最终视频生成按钮正确判断**: `canGenerateFinalVideo` 函数检查所有场景的所有关键帧图片任务状态

---

## 相关文件

- `cmd/story-video-server/main.go` - 主服务器代码，包含内嵌 HTML/JS 和 Go 后端逻辑
- `projects/{projectID}/scenes/{sceneID}.json` - 单个场景数据文件
- `projects/{projectID}/scenes/index.json` - 场景索引文件
- `projects/{projectID}/tasks.json` - 任务列表文件
