# 儿童故事视频生成系统技术方案（无 OpenClaw 版本）

## 1. 项目概述

### 1.1 项目目标

本方案面向产品级落地，目标是将一段儿童故事文本自动生成一个可播放的视频成品，最终产物包含：

- 场景插画
- 镜头动效
- TTS 旁白配音
- 字幕
- 背景音乐
- 最终 MP4 视频文件

系统不使用 OpenClaw，不构建复杂 Agent 链路，而采用“弱模型 + 强规则”的工程化媒体生产流水线。

### 1.2 核心设计原则

#### 原则 1：一次模型，后续规则执行

LLM 只在入口处调用一次，用于将原始故事文本转换成结构化的 `storyboard.json`。后续图片生成、配音、字幕、视频合成、BGM 混音、文件上传等步骤全部走确定性工程流水线，不再依赖 LLM 进行动态决策。

#### 原则 2：每个 Scene 独立可执行

每个场景都可以独立完成以下操作：

- 生成场景图
- 生成旁白音频
- 生成字幕
- 合成短视频片段
- 失败后单独重试

#### 原则 3：配置驱动，不写死流程

系统通过 `storyboard.json` 中的 `video_profile`、`audio_profile`、`render_rules` 等配置控制行为，而不是把业务逻辑硬编码在 FFmpeg 命令或 Worker 内部。

#### 原则 4：工程可重试、可监控、可扩展

系统需要支持：

- Scene 级失败重试
- Step 级失败重试
- 项目状态跟踪
- 中间产物落盘
- 错误日志记录
- 后续扩展模板、多语言、批量生成和后台运营能力

## 2. 整体技术方案

### 2.1 总体架构

```text
┌────────────────────────────────────────────┐
│                Frontend / Admin            │
│   Web 页面 / 后台管理 / API 调用入口       │
└────────────────────────────────────────────┘
                      │
                      ▼
┌────────────────────────────────────────────┐
│              Backend API Layer             │
│  - 创建项目                                │
│  - 查询状态                                │
│  - 触发生成                                │
│  - 重试 scene                              │
│  - 下载成品                                │
└────────────────────────────────────────────┘
                      │
                      ▼
┌────────────────────────────────────────────┐
│            Orchestrator / Scheduler        │
│  - 项目状态机                              │
│  - 任务拆分                                │
│  - Worker 调度                             │
│  - 失败重试                                │
│  - 产物汇总                                │
└────────────────────────────────────────────┘
          │               │               │
          ▼               ▼               ▼
┌────────────────┐ ┌────────────────┐ ┌────────────────┐
│ LLM Service    │ │ Image Worker   │ │ TTS Worker     │
│ 仅一次调用     │ │ 场景图生成      │ │ 配音+字幕时间轴 │
└────────────────┘ └────────────────┘ └────────────────┘
                              │
                              ▼
                    ┌────────────────────┐
                    │ FFmpeg Worker      │
                    │ scene合成/拼接/BGM │
                    └────────────────────┘
                              │
                              ▼
                    ┌────────────────────┐
                    │ Storage & Database  │
                    │ DB / OSS / Queue    │
                    └────────────────────┘
```

### 2.2 核心链路

```text
原始故事文本
  ↓
LLM 生成 storyboard.json
  ↓
Orchestrator 校验 storyboard.json
  ↓
按 scene 批量生成图片
  ↓
按 scene 批量生成 TTS 音频与字幕
  ↓
按 scene 生成短视频片段
  ↓
拼接全部 scene
  ↓
叠加 BGM / 输出 final_video.mp4
  ↓
上传存储 / 返回结果
```

## 3. 流程图设计

### 3.1 主流程图

```text
[用户提交故事]
      ↓
[创建 Project]
      ↓
[调用 LLM 生成 storyboard.json]
      ↓
[JSON Schema 校验]
      ↓
 ┌────校验失败────┐
 ↓                │
[进入失败态]      │
                  │
          [校验通过]
                  ↓
        [创建 scene 任务]
                  ↓
      [批量生成 scene 图片]
                  ↓
      [批量生成 scene 音频]
                  ↓
      [批量生成 scene 字幕]
                  ↓
      [批量生成 scene 视频]
                  ↓
        [合成 final video]
                  ↓
          [叠加 BGM]
                  ↓
          [上传存储]
                  ↓
          [更新项目状态]
                  ↓
            [返回成品]
```

### 3.2 Scene 级流程图

```text
[读取 scene 配置]
      ↓
[检查 image 是否存在]
      ↓
[若不存在则调用 Image Worker]
      ↓
[检查 audio 是否存在]
      ↓
[若不存在则调用 TTS Worker]
      ↓
[生成 subtitle 文件]
      ↓
[FFmpeg 合成 scene 视频]
      ↓
[输出 scene_xxx.mp4]
      ↓
[标记 scene 完成]
```

### 3.3 重试流程图

```text
[scene 失败]
    ↓
[记录 error_code / error_message]
    ↓
[判断是否可重试]
    ├─ 是 → [retry_count + 1] → [重新执行当前 step]
    └─ 否 → [标记 FAILED_NEEDS_HUMAN]
```

## 4. 模块设计

### 4.1 Frontend / Admin 模块

职责：

- 提交故事文本
- 选择风格、时长、音色
- 查看项目状态
- 预览 `storyboard.json`
- 下载成品视频
- 对失败任务发起重试

核心页面：

- 项目创建页
- 任务进度页
- 项目详情页
- 资源下载页
- 后台运营页

### 4.2 Backend API 模块

职责：

- 对外提供 REST API
- 验证请求参数
- 创建项目
- 查询项目状态
- 返回资源地址
- 提供管理操作

推荐技术：

- Go
- Python FastAPI
- Node.js

首版建议使用 Go 或 FastAPI。

### 4.3 Orchestrator 模块

Orchestrator 是系统核心中枢。

职责：

- 推进项目状态机
- 调用 LLM 生成 `storyboard.json`
- 拆分 Scene 任务
- 调度 Image Worker、TTS Worker、FFmpeg Worker
- 聚合结果
- 执行失败重试
- 写入数据库

核心能力：

- 项目状态推进
- Scene 并发调度
- 步骤级幂等
- 错误补偿

推荐实现：

- 代码内状态机 + 队列
- 首版不需要引入重型 Workflow 框架

### 4.4 LLM Service 模块

职责：

- 输入原始故事
- 输出 `storyboard.json`

注意：这里的 LLM 只调用一次。

输入：

- 标题
- 原始故事
- 目标年龄
- 风格
- 目标时长

输出：

- `storyboard.json`

关键要求：

- 输出必须符合 JSON Schema
- 不允许返回自然语言解释
- 只允许返回 JSON

### 4.5 Storyboard Validator 模块

职责：

- 校验 `storyboard.json` 是否满足执行要求

校验内容：

- 顶层字段完整
- `scene_id` 唯一
- 每个 Scene 都有 `narration`、`subtitle`、`prompt`
- 配置字段合法
- 场景数量合理
- `duration_hint_sec` 不为空

输出：

```json
{
  "valid": true,
  "errors": []
}
```

### 4.6 Image Worker 模块

职责：

- 根据 `storyboard.json` 中的 Prompt 生成场景图

输入：

- `scene.prompt.subject_prompt`
- `scene.prompt.scene_prompt`
- `character_bible`
- `global_style`
- `negative_prompt`

输出：

- `scene.image_url`
- `scene.image_local_path`

关键设计：

- 后端拼接 `full_prompt`
- 每个 Scene 独立生成
- 支持重试
- 支持缓存命中
- 不再次调用 LLM 写 Prompt

### 4.7 TTS Worker 模块

职责：

- 根据 `narration` 生成音频
- 获取真实音频时长
- 生成字幕时间轴

输入：

- `scene.narration`
- `audio_profile`

输出：

- `scene.audio_url`
- `scene.audio_duration_ms`
- `scene.subtitle_path`

关键设计：

- 音频按 Scene 粒度生成
- 字幕按 Sentence 粒度生成
- Scene 时长以真实音频时长为准

### 4.8 Subtitle Builder 模块

职责：

- 将 TTS 边界时间转换为 `.srt`
- 生成 Scene 级字幕文件

输出：

- `scene_001.srt`

设计建议：

- 首版先做 Scene 级烧录字幕，简单稳定

### 4.9 FFmpeg Worker 模块

职责：

- 单 Scene 合成视频
- 多 Scene 拼接
- 自动转场
- 字幕烧录
- BGM 混音
- 输出 final video

输入：

- image
- audio
- subtitle
- `video_profile`
- `render_rules`

输出：

- `scene_xxx.mp4`
- `final_video.mp4`

支持能力：

- `slow_zoom_in`
- `slow_zoom_out`
- `pan_left`
- `pan_right`
- `floating_drift`
- fade 转场
- cut 拼接
- BGM 混音

### 4.10 Storage 模块

职责：

- 存储中间文件
- 存储成品
- 提供下载地址
- 管理生命周期

存储内容：

- `storyboard.json`
- Scene 图片
- Scene 音频
- Scene 字幕
- Scene 视频
- final 视频

推荐：

- 本地开发：本地磁盘
- 生产环境：OSS / S3

### 4.11 Database 模块

职责：

- 保存项目状态
- 保存 Scene 状态
- 保存 Task 状态
- 保存资源元数据
- 保存错误日志

推荐表：

- `projects`
- `project_storyboards`
- `scenes`
- `scene_assets`
- `tasks`
- `task_logs`

### 4.12 Notification 模块

职责：

- 项目完成通知
- 失败通知
- 后台告警

通知方式：

- WebSocket
- 站内通知
- 邮件
- IM Webhook

## 5. 协议设计

### 5.1 顶层协议：storyboard.json

```json
{
  "meta": {},
  "project": {},
  "global_style": {},
  "character_bible": {},
  "audio_profile": {},
  "video_profile": {},
  "render_rules": {},
  "scenes": []
}
```

### 5.2 project 协议

```json
{
  "project": {
    "project_id": "pv_0001",
    "title": "小云朵的星星收集之旅",
    "language": "zh-CN",
    "target_age": "4-6",
    "theme": "bedtime_story",
    "tone": "gentle, warm, magical",
    "target_duration_sec": 600,
    "aspect_ratio": "16:9",
    "resolution": "1920x1080"
  }
}
```

### 5.3 global_style 协议

```json
{
  "global_style": {
    "visual_style": "children storybook illustration",
    "render_style": "soft 2D animated illustration",
    "palette": ["pastel blue", "warm yellow", "soft pink", "lavender"],
    "lighting": "soft dreamy night lighting",
    "mood": "calm, cozy, magical, bedtime",
    "quality_tags": [
      "clean composition",
      "consistent character design",
      "gentle expressions"
    ],
    "negative_prompt": [
      "horror",
      "violence",
      "blurry",
      "low quality"
    ]
  }
}
```

### 5.4 character_bible 协议

```json
{
  "character_bible": {
    "main_characters": [
      {
        "character_id": "char_mianmian",
        "name": "棉棉",
        "prompt_anchor": "a fluffy white cloud character named Mianmian, soft rounded shape, cute small eyes, warm smile"
      }
    ],
    "recurring_objects": [
      {
        "object_id": "obj_star",
        "name": "小星星",
        "prompt_anchor": "tiny glowing stars with cute magical sparkle"
      }
    ]
  }
}
```

### 5.5 audio_profile 协议

```json
{
  "audio_profile": {
    "voice_language": "zh-CN",
    "voice_name": "zh-CN-XiaoyiNeural",
    "speaking_rate": "-10%",
    "pitch": "-2%",
    "volume": "+0%",
    "style": "bedtime_gentle",
    "pause_strategy": {
      "sentence_pause_ms": 300,
      "paragraph_pause_ms": 700
    },
    "bgm": {
      "track_id": "bedtime_piano_001",
      "volume_db": -18
    }
  }
}
```

### 5.6 video_profile 协议

```json
{
  "video_profile": {
    "fps": 24,
    "transition": "fade",
    "transition_duration_ms": 500,
    "subtitle_style": {
      "font_size": 42,
      "position": "bottom-center",
      "max_lines": 2
    }
  }
}
```

### 5.7 render_rules 协议

```json
{
  "render_rules": {
    "image_generation": {
      "images_per_scene": 1,
      "retry_limit": 3
    },
    "camera_motion_defaults": {
      "allowed": [
        "slow_zoom_in",
        "slow_zoom_out",
        "pan_left",
        "pan_right",
        "floating_drift"
      ],
      "fallback": "slow_zoom_in"
    },
    "scene_duration_policy": "tts_audio_based",
    "subtitle_policy": "sentence_based",
    "asset_naming_policy": "project_scene_step"
  }
}
```

### 5.8 scene 协议

```json
{
  "scene_id": "scene_001",
  "sequence": 1,
  "title": "天空上的棉棉",
  "story_function": "opening",
  "narration": "在很远很远的天空上，住着一朵软乎乎的小云朵，名字叫棉棉。",
  "subtitle": "在很远很远的天空上，住着一朵软乎乎的小云朵，名字叫棉棉。",
  "duration_hint_sec": 8,
  "characters": ["char_mianmian"],
  "objects": ["obj_star"],
  "environment": {
    "location": "night_sky",
    "time": "night",
    "weather": "clear",
    "atmosphere": "dreamy and peaceful"
  },
  "visual": {
    "scene_description": "Mianmian floating in a dreamy starry sky",
    "composition": "medium wide shot",
    "camera_motion": "slow_zoom_in",
    "focus": "introduce the main character gently"
  },
  "prompt": {
    "subject_prompt": "a fluffy white cloud character named Mianmian floating in the dreamy night sky",
    "scene_prompt": "soft stars scattered across the sky, calm bedtime atmosphere",
    "full_prompt": "",
    "negative_prompt_override": []
  },
  "audio": {
    "voice_name": "zh-CN-XiaoyiNeural",
    "speaking_rate": "-10%"
  },
  "effects": {
    "sfx": ["soft wind", "tiny sparkle"],
    "bgm_mood": "gentle magical"
  }
}
```

## 6. Worker 间协议设计

### 6.1 Image Worker 请求协议

请求：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "full_prompt": "....",
  "negative_prompt": "....",
  "resolution": "1920x1080",
  "retry_limit": 3
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "status": "success",
  "image_url": "oss://bucket/pv_0001/scene_001.png",
  "image_local_path": "/data/projects/pv_0001/images/scene_001.png"
}
```

### 6.2 TTS Worker 请求协议

请求：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "text": "在很远很远的天空上，住着一朵软乎乎的小云朵，名字叫棉棉。",
  "voice_name": "zh-CN-XiaoyiNeural",
  "speaking_rate": "-10%",
  "pitch": "-2%"
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "status": "success",
  "audio_url": "oss://bucket/pv_0001/audio/scene_001.mp3",
  "audio_duration_ms": 7420,
  "subtitle_url": "oss://bucket/pv_0001/subtitles/scene_001.srt"
}
```

### 6.3 FFmpeg Worker 请求协议

请求：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "image_path": "/data/projects/pv_0001/assets/images/scene_001.png",
  "audio_path": "/data/projects/pv_0001/assets/audio/scene_001.mp3",
  "subtitle_path": "/data/projects/pv_0001/assets/subtitles/scene_001.srt",
  "resolution": "1920x1080",
  "fps": 24,
  "camera_motion": "slow_zoom_in",
  "transition": "fade"
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "status": "success",
  "scene_video_path": "/data/projects/pv_0001/output/scenes/scene_001.mp4",
  "scene_duration_ms": 7720
}
```

## 7. Web 模型 Provider 协议设计

### 7.1 设计背景

为了节省 Token 与模型 API 成本，系统可以不直接调用官方付费 API，而是接入类似 `doubao`、`qwen-web` 的 Web 渠道。

这里需要强调：Web 渠道不是让业务流程重新变成 Agent，而是把“模型能力”抽象成一个可替换的 Provider。业务层仍然只看到统一协议：

```text
storyboard_generator.generate()
image_generator.generate()
```

至于底层是：

- 官方 API
- Web 自动化
- 本地模型
- 第三方网关
- 未来的 `qwen-web`

都由 Provider Adapter 负责隔离。

### 7.2 核心抽象

推荐采用 `provider/model` 形式标识模型能力，类似：

```text
doubao/web
qwen-web/wanxiang
openai/gpt-image-1
local/sd-xl
```

业务层只保存模型引用，不直接保存具体网页操作细节。

```json
{
  "provider_ref": "doubao/web",
  "capability": "image_generation"
}
```

### 7.3 Provider 类型

系统建议统一抽象三类 Provider：

| Provider 类型 | 用途 | 示例 |
| --- | --- | --- |
| `storyboard_provider` | 生成 `storyboard.json` | `doubao/web`, `qwen-web/chat`, `openai/gpt-5.4` |
| `image_provider` | 生成 Scene 图片 | `doubao/web`, `qwen-web/wanxiang`, `local/sd-xl` |
| `tts_provider` | 生成音频 | `edge-tts/zh-CN-XiaoyiNeural`, `volc-tts/*` |

### 7.4 Provider Registry 协议

Provider Registry 用于注册可用供应商、模型和能力。

```json
{
  "providers": [
    {
      "provider_id": "doubao",
      "label": "Doubao",
      "type": "web",
      "enabled": true,
      "aliases": ["doubao-web"],
      "auth_profile_id": "auth_doubao_main",
      "capabilities": {
        "storyboard_generation": {
          "enabled": true,
          "models": ["web"]
        },
        "image_generation": {
          "enabled": true,
          "models": ["web"],
          "max_count": 4,
          "supports_aspect_ratio": true,
          "supports_resolution": true,
          "supported_aspect_ratios": ["16:9", "9:16", "1:1"],
          "supported_resolutions": ["1K", "2K"]
        }
      },
      "runtime": {
        "adapter": "browser_automation",
        "entrypoint": "providers/doubao_web",
        "timeout_ms": 180000,
        "concurrency": 1,
        "rate_limit": {
          "requests_per_minute": 3
        }
      }
    }
  ]
}
```

设计要点：

- `provider_id` 是稳定 ID，不随模型变化
- `aliases` 用于兼容旧配置
- `capabilities` 描述能力，而不是在业务代码里写 `if doubao`
- `auth_profile_id` 指向登录态或密钥配置
- `runtime.adapter` 决定使用官方 API、本地模型还是浏览器自动化

### 7.5 Auth Profile 协议

Web 渠道通常不使用 API Key，而是使用 Cookie、登录态、浏览器 Profile 或远程浏览器会话。

```json
{
  "auth_profile_id": "auth_doubao_main",
  "provider_id": "doubao",
  "auth_type": "browser_profile",
  "status": "ACTIVE",
  "storage": {
    "profile_dir": "/data/browser-profiles/doubao-main",
    "cookie_path": "/data/secrets/doubao/cookies.json"
  },
  "health": {
    "last_checked_at": "2026-04-22T10:00:00+08:00",
    "login_valid": true,
    "quota_available": true
  }
}
```

可选 `auth_type`：

- `api_key`
- `browser_profile`
- `cookie`
- `oauth`
- `local_none`

### 7.6 Storyboard Provider 请求协议

业务层向 Provider 发起请求时，不关心底层是 API 还是 Web。

请求：

```json
{
  "request_id": "req_storyboard_0001",
  "project_id": "pv_0001",
  "capability": "storyboard_generation",
  "provider_ref": "doubao/web",
  "input": {
    "title": "小云朵的星星收集之旅",
    "story_text": "在很远很远的天空上...",
    "language": "zh-CN",
    "target_age": "4-6",
    "style": "bedtime_story",
    "target_duration_sec": 600
  },
  "output_contract": {
    "format": "json",
    "schema_ref": "storyboard.schema.v1",
    "strict_json": true
  },
  "runtime_options": {
    "timeout_ms": 180000,
    "retry_limit": 2
  }
}
```

返回：

```json
{
  "request_id": "req_storyboard_0001",
  "project_id": "pv_0001",
  "provider_ref": "doubao/web",
  "status": "success",
  "output": {
    "storyboard_json": {}
  },
  "usage": {
    "billing_mode": "web",
    "api_tokens": 0,
    "estimated_cost": 0
  },
  "raw_artifacts": {
    "response_text_path": "/data/projects/pv_0001/raw/storyboard_response.txt",
    "screenshot_path": "/data/projects/pv_0001/debug/storyboard_doubao.png"
  }
}
```

### 7.7 Image Provider 请求协议

请求：

```json
{
  "request_id": "req_image_scene_001",
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "capability": "image_generation",
  "provider_ref": "doubao/web",
  "input": {
    "prompt": "a fluffy white cloud character named Mianmian...",
    "negative_prompt": "horror, violence, blurry, low quality",
    "aspect_ratio": "16:9",
    "resolution": "2K",
    "count": 1,
    "seed": null,
    "reference_images": []
  },
  "output_contract": {
    "mime_types": ["image/png", "image/jpeg"],
    "min_width": 1280,
    "min_height": 720,
    "save_policy": "project_scene_asset"
  },
  "runtime_options": {
    "timeout_ms": 300000,
    "retry_limit": 3
  }
}
```

返回：

```json
{
  "request_id": "req_image_scene_001",
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "provider_ref": "doubao/web",
  "status": "success",
  "images": [
    {
      "asset_id": "asset_img_scene_001_01",
      "mime_type": "image/png",
      "width": 1920,
      "height": 1080,
      "local_path": "/data/projects/pv_0001/images/scene_001.png",
      "url": "oss://bucket/pv_0001/images/scene_001.png",
      "revised_prompt": null,
      "metadata": {
        "provider_job_id": "web_job_abc",
        "download_source": "browser"
      }
    }
  ],
  "usage": {
    "billing_mode": "web",
    "api_tokens": 0,
    "estimated_cost": 0
  },
  "raw_artifacts": {
    "screenshot_path": "/data/projects/pv_0001/debug/image_scene_001_doubao.png"
  }
}
```

### 7.8 Provider Adapter 接口

每个 Provider 只需要实现统一接口。

```ts
type ProviderCapability =
  | "storyboard_generation"
  | "image_generation"
  | "tts";

type ProviderRef = {
  provider: string;
  model: string;
};

interface ModelProviderAdapter {
  id: string;
  aliases?: string[];
  capabilities: ProviderCapability[];

  healthCheck(): Promise<ProviderHealth>;

  generateStoryboard?(
    request: StoryboardProviderRequest
  ): Promise<StoryboardProviderResult>;

  generateImage?(
    request: ImageProviderRequest
  ): Promise<ImageProviderResult>;

  generateTts?(
    request: TtsProviderRequest
  ): Promise<TtsProviderResult>;
}
```

`doubao` 与 `qwen-web` 的差异只存在于 Adapter 内部：

```text
业务层
  ↓
Provider Runtime
  ↓
Provider Adapter: doubao / qwen-web / openai / local
  ↓
具体执行：浏览器自动化 / HTTP API / 本地推理
```

### 7.9 Web Adapter 内部执行协议

Web 自动化类 Provider 建议内部拆成固定步骤：

```text
prepare_browser_session
→ validate_login
→ open_generation_page
→ fill_prompt
→ submit_job
→ wait_until_done
→ collect_outputs
→ download_assets
→ normalize_result
→ persist_raw_artifacts
```

每一步都需要可观测、可重试、可记录错误码。

推荐错误码：

| 错误码 | 含义 |
| --- | --- |
| `WEB_LOGIN_EXPIRED` | 登录态失效 |
| `WEB_PAGE_CHANGED` | 页面结构变化 |
| `WEB_RATE_LIMITED` | Web 端限流 |
| `WEB_CAPTCHA_REQUIRED` | 需要人工验证码 |
| `WEB_GENERATION_TIMEOUT` | 生成超时 |
| `WEB_DOWNLOAD_FAILED` | 产物下载失败 |
| `WEB_OUTPUT_INVALID` | 输出不满足协议 |

### 7.10 storyboard.json 中的 Provider 配置

`storyboard.json` 不应该写死 `doubao` 的页面细节，只需要保存能力选择。

```json
{
  "model_profile": {
    "storyboard_provider_ref": "doubao/web",
    "image_provider_ref": "doubao/web",
    "tts_provider_ref": "edge-tts/zh-CN-XiaoyiNeural",
    "fallbacks": {
      "storyboard_generation": ["qwen-web/chat", "openai/gpt-5.4"],
      "image_generation": ["qwen-web/wanxiang", "local/sd-xl"]
    }
  }
}
```

后续如果切换到 `qwen-web`，只需要改：

```json
{
  "image_provider_ref": "qwen-web/wanxiang"
}
```

业务流程不用改。

### 7.11 任务表扩展字段

为了追踪 Web Provider 执行过程，`tasks` 表建议增加以下字段：

```sql
ALTER TABLE tasks
  ADD COLUMN provider_ref VARCHAR(128),
  ADD COLUMN provider_request_id VARCHAR(128),
  ADD COLUMN billing_mode VARCHAR(32),
  ADD COLUMN raw_artifacts JSON;
```

`scene_assets` 表建议增加：

```sql
ALTER TABLE scene_assets
  ADD COLUMN provider_ref VARCHAR(128),
  ADD COLUMN provider_job_id VARCHAR(128),
  ADD COLUMN asset_metadata JSON;
```

### 7.12 设计结论

这层协议的关键不是“绑定 doubao”，而是抽象出稳定的模型能力接口：

```text
Provider Registry
+ Provider Ref
+ Auth Profile
+ Capability Schema
+ Adapter Runtime
+ Normalized Result
```

这样首版可以接 `doubao` 节省成本，后续也可以平滑切换到：

- `qwen-web`
- 官方 API
- 本地图像模型
- 第三方聚合网关

Orchestrator 和 Worker 不需要知道页面细节，只消费统一请求与统一返回。

### 7.13 ZeroToken Web Runtime 设计

参考同级目录 `openclaw-zero-token` 的实现，Web 模型接入不应只设计成一种方式。实际可用方式至少包括：

| 方式 | 说明 | 适用 Provider |
| --- | --- | --- |
| `browser_dom` | 通过 Debug Chrome / CDP 连接真实浏览器，模拟输入、点击、等待回复 | Doubao、Gemini、Claude、ChatGPT、Kimi、GLM、Xiaomi |
| `browser_eval_fetch` | 通过 `page.evaluate()` 在网页上下文内执行 `fetch`，复用页面登录态与浏览器环境 | Qwen、GLM、部分 Chat Web |
| `browser_network_capture` | DOM 触发真实请求，同时监听 Web Response / SSE，再解析返回内容 | Doubao 文本、Doubao 图片 |
| `web_http_replay` | 从浏览器提取 Cookie、User-Agent、动态参数后，由后端复刻 Web 请求 | Doubao 旧接口、部分 Cookie 型站点 |
| `hybrid` | 优先走页面内请求或 HTTP replay，失败后降级 DOM 模拟 | Qwen、Doubao、DeepSeek 等 |

因此系统中建议增加一个独立组件：

```text
ZeroToken Web Runtime
  ├── Browser Session Manager
  ├── CDP Connector
  ├── DOM Driver
  ├── Browser Eval Fetch Driver
  ├── Network Capture Driver
  ├── Web HTTP Replay Driver
  ├── Provider Adapters
  └── Result Normalizer
```

业务侧仍然只调用：

```text
generateStoryboard(provider_ref, payload)
generateImage(provider_ref, payload)
```

底层使用哪一种 Web 方式，由 Provider Registry 的 `transport_strategy` 决定。

### 7.14 Browser Profile 协议

Debug Chrome 方式需要把浏览器会话抽象出来，避免每个 Provider 自己管理 Chrome。

```json
{
  "browser_profiles": [
    {
      "profile_id": "chrome_main",
      "enabled": true,
      "mode": "attach_only",
      "cdp_url": "http://127.0.0.1:9222",
      "user_data_dir": "/data/browser-profiles/chrome-main",
      "headless": false,
      "default_timeout_ms": 120000,
      "security": {
        "allow_remote_cdp": false,
        "allowed_hosts": ["127.0.0.1", "localhost"]
      }
    }
  ]
}
```

字段说明：

- `mode=attach_only`：连接用户已启动的 Debug Chrome
- `mode=managed`：由系统自动启动 Chrome
- `cdp_url`：Chrome DevTools Protocol 地址
- `user_data_dir`：持久化登录态
- `headless=false`：首版建议可视化，方便人工登录和排错

### 7.15 Web Provider Registry 扩展

为兼容 openclaw-zero-token 支持的多种 Web 方式，Provider 配置建议增加 `transport_strategy`。

```json
{
  "provider_id": "doubao",
  "label": "Doubao",
  "type": "web",
  "enabled": true,
  "auth_profile_id": "auth_doubao_main",
  "browser_profile_id": "chrome_main",
  "transport_strategy": {
    "primary": "browser_network_capture",
    "fallbacks": ["browser_dom", "web_http_replay"],
    "requires_cdp": true,
    "requires_visible_browser": true
  },
  "capabilities": {
    "storyboard_generation": {
      "enabled": true,
      "models": ["web"]
    },
    "image_generation": {
      "enabled": true,
      "models": ["web"],
      "max_count": 4,
      "supported_aspect_ratios": ["1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9"],
      "supported_resolutions": ["1K", "2K", "4K"]
    }
  }
}
```

其他 Provider 示例：

```json
[
  {
    "provider_id": "qwen-web",
    "browser_profile_id": "chrome_main",
    "transport_strategy": {
      "primary": "browser_eval_fetch",
      "fallbacks": ["browser_dom"],
      "requires_cdp": true
    },
    "capabilities": {
      "storyboard_generation": {
        "enabled": true,
        "models": ["qwen3.5-plus"]
      }
    }
  },
  {
    "provider_id": "gemini-web",
    "browser_profile_id": "chrome_main",
    "transport_strategy": {
      "primary": "browser_dom",
      "fallbacks": [],
      "requires_cdp": true
    },
    "capabilities": {
      "storyboard_generation": {
        "enabled": true,
        "models": ["gemini-web"]
      }
    }
  },
  {
    "provider_id": "glm-web",
    "browser_profile_id": "chrome_main",
    "transport_strategy": {
      "primary": "browser_eval_fetch",
      "fallbacks": ["browser_dom"],
      "requires_cdp": true
    },
    "capabilities": {
      "storyboard_generation": {
        "enabled": true,
        "models": ["glm-web"]
      }
    }
  }
]
```

### 7.16 DOM Driver 协议

DOM 模拟类 Provider 需要把页面操作抽象成配置，而不是散落在业务代码里。

```json
{
  "dom_driver": {
    "start_url": "https://www.doubao.com/chat/",
    "page_url_patterns": ["doubao.com/chat"],
    "input_selectors": [
      "textarea",
      "div[contenteditable='true']",
      "[role='textbox']"
    ],
    "send_actions": [
      {
        "type": "keyboard",
        "key": "Enter"
      }
    ],
    "pre_actions": [
      {
        "type": "click_if_exists",
        "selector": "[data-testid='text-mode']",
        "timeout_ms": 3000
      }
    ],
    "wait_policy": {
      "type": "stable_text",
      "max_wait_ms": 120000,
      "poll_interval_ms": 2000,
      "stable_rounds": 3
    },
    "output_extractors": [
      {
        "type": "main_text",
        "selector": "main",
        "exclude_selectors": ["nav", "form", "[class*='sidebar']"]
      }
    ]
  }
}
```

DOM Driver 标准步骤：

```text
connect_cdp
→ find_or_open_page
→ validate_login
→ run_pre_actions
→ locate_input
→ fill_prompt
→ send
→ wait_completion
→ extract_output
→ normalize_result
```

### 7.17 Browser Eval Fetch Driver 协议

部分站点可以通过 `page.evaluate()` 在网页上下文里发请求。这样能复用浏览器 Cookie、LocalStorage、CSRF、User-Agent 和站点运行时环境。

```json
{
  "browser_eval_fetch": {
    "start_url": "https://chat.qwen.ai/",
    "bootstrap_steps": [
      {
        "name": "create_chat",
        "method": "POST",
        "url": "/api/v2/chats/new",
        "body": {}
      }
    ],
    "send_message": {
      "method": "POST",
      "url_template": "/api/v2/chats/{chat_id}/messages",
      "body_template": {
        "stream": true,
        "model": "{{model}}",
        "messages": [
          {
            "role": "user",
            "content": "{{prompt}}"
          }
        ]
      }
    },
    "response_mode": "stream",
    "output_path": "$.data.content"
  }
}
```

标准步骤：

```text
connect_cdp
→ open_provider_page
→ validate_login
→ execute_bootstrap_fetch
→ execute_send_fetch_in_page_context
→ parse_stream_or_json
→ normalize_result
```

### 7.18 Network Capture Driver 协议

Doubao 图片生成这类场景适合让页面自己发请求，然后由 Runtime 捕获响应并解析 SSE / JSON。

```json
{
  "network_capture": {
    "trigger": "dom",
    "listen": [
      {
        "name": "doubao_chat_completion",
        "method": "POST",
        "url_contains": "doubao.com/chat/completion",
        "content_type_contains": "text/event-stream",
        "timeout_ms": 120000
      }
    ],
    "parsers": [
      {
        "type": "sse",
        "events": ["CHUNK_DELTA", "STREAM_CHUNK", "SSE_REPLY_END"]
      },
      {
        "type": "doubao_creation_block",
        "extract": "image_urls"
      }
    ]
  }
}
```

标准步骤：

```text
connect_cdp
→ open_provider_page
→ attach_response_listener
→ trigger_dom_action
→ wait_target_response
→ parse_sse_events
→ extract_text_or_image_urls
→ download_assets
→ normalize_result
```

### 7.19 Web HTTP Replay Driver 协议

有些站点可以从浏览器里提取 Cookie、User-Agent、动态参数，再由后端直接发 HTTP 请求。这个方式速度快，但对站点反爬参数更敏感，应作为可选策略。

```json
{
  "web_http_replay": {
    "base_url": "https://www.doubao.com",
    "endpoint": "/samantha/chat/completion",
    "method": "POST",
    "auth_sources": ["cookie", "user_agent", "query_params"],
    "dynamic_params": ["msToken", "a_bogus", "fp", "tea_uuid", "device_id", "web_tab_id"],
    "response_mode": "sse",
    "parser": "doubao_sse_text"
  }
}
```

标准步骤：

```text
load_auth_profile
→ refresh_dynamic_params_from_browser
→ build_headers
→ build_query_params
→ send_http_request
→ parse_sse_or_json
→ normalize_result
```

### 7.20 支持的 Web Provider 初始清单

结合 `openclaw-zero-token/src/zero-token/providers` 当前已有实现，首版可以按以下 Provider 规划：

| Provider | 推荐主策略 | 能力 | 备注 |
| --- | --- | --- | --- |
| `doubao` | `browser_network_capture` | 文本、图片 | Web 模型统一承载文本与图片能力，图片支持多比例 |
| `qwen-web` | `browser_eval_fetch` | 文本 | 可降级 DOM |
| `qwen-cn-web` | `browser_eval_fetch` | 文本 | 国内 Qwen 站点适配 |
| `gemini-web` | `browser_dom` | 文本 | DOM 模拟输入与轮询回复 |
| `chatgpt-web` | `browser_dom` | 文本 | 依赖登录态 |
| `claude-web` | `browser_dom` 或 Web client | 文本 | 依赖登录态 |
| `deepseek-web` | `hybrid` | 文本 | 视站点接口稳定性选择 |
| `kimi-web` | `browser_dom` | 文本 | 适合 Storyboard 生成 |
| `glm-web` | `browser_eval_fetch` | 文本 | 国内 GLM |
| `glm-intl-web` | `browser_eval_fetch` | 文本 | 国际版 GLM / Z.ai |
| `grok-web` | `browser_dom` | 文本 | 依赖浏览器登录 |
| `perplexity-web` | `browser_dom` | 文本 | 可作为辅助生成 |
| `xiaomimo-web` | `browser_dom` | 文本 | 小米系 Web |

视频生产系统首版建议只启用：

- `doubao`：Storyboard + 图片
- `qwen-web`：Storyboard fallback
- `gemini-web`：Storyboard fallback
- `glm-web`：Storyboard fallback

后续再逐步开放 ChatGPT、Claude、Kimi、DeepSeek 等 Provider。

### 7.21 统一执行请求协议

无论底层使用哪种 Web 方式，Orchestrator 都只提交统一请求。

```json
{
  "request_id": "req_zero_token_0001",
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "provider_ref": "doubao/web",
  "capability": "image_generation",
  "transport_preference": ["browser_network_capture", "browser_dom", "web_http_replay"],
  "input": {
    "prompt": "a fluffy white cloud character...",
    "aspect_ratio": "16:9",
    "resolution": "2K",
    "count": 1
  },
  "runtime_options": {
    "browser_profile_id": "chrome_main",
    "timeout_ms": 300000,
    "retry_limit": 3,
    "save_debug_artifacts": true
  }
}
```

统一返回：

```json
{
  "request_id": "req_zero_token_0001",
  "provider_ref": "doubao/web",
  "transport_used": "browser_network_capture",
  "status": "success",
  "output": {
    "text": null,
    "images": [
      {
        "url": "https://p11-flow-imagex-sign.byteimg.com/xxx.jpeg",
        "local_path": "/data/projects/pv_0001/images/scene_001.png",
        "width": 2048,
        "height": 2048,
        "mime_type": "image/jpeg"
      }
    ]
  },
  "debug": {
    "cdp_url": "http://127.0.0.1:9222",
    "page_url": "https://www.doubao.com/chat/",
    "screenshot_path": "/data/projects/pv_0001/debug/scene_001.png",
    "raw_response_path": "/data/projects/pv_0001/debug/scene_001.sse"
  },
  "usage": {
    "billing_mode": "web",
    "api_tokens": 0,
    "estimated_cost": 0
  }
}
```

### 7.22 Web Runtime 错误码

```text
BROWSER_CDP_UNAVAILABLE
BROWSER_PROFILE_NOT_FOUND
BROWSER_LOGIN_EXPIRED
BROWSER_PAGE_OPEN_FAILED
DOM_INPUT_NOT_FOUND
DOM_SEND_FAILED
DOM_OUTPUT_TIMEOUT
EVAL_FETCH_FAILED
NETWORK_RESPONSE_TIMEOUT
SSE_PARSE_FAILED
IMAGE_URL_NOT_FOUND
IMAGE_DOWNLOAD_FAILED
WEB_HTTP_REPLAY_FAILED
WEB_DYNAMIC_PARAM_EXPIRED
WEB_CAPTCHA_REQUIRED
WEB_RATE_LIMITED
WEB_PAGE_CHANGED
```

处理建议：

- `BROWSER_LOGIN_EXPIRED`、`WEB_CAPTCHA_REQUIRED` 标记为 `FAILED_NEEDS_HUMAN`
- `DOM_OUTPUT_TIMEOUT`、`NETWORK_RESPONSE_TIMEOUT` 可重试
- `WEB_PAGE_CHANGED` 需要更新 Provider Adapter
- `WEB_DYNAMIC_PARAM_EXPIRED` 可尝试刷新浏览器页面后重试

### 7.23 和视频生产流水线的关系

ZeroToken Web Runtime 只是模型能力适配层，不改变主流程。

```text
Orchestrator
  ↓
Image Worker / Storyboard Worker
  ↓
Provider Runtime
  ↓
ZeroToken Web Runtime
  ↓
Debug Chrome / DOM / page.evaluate / Network Capture / HTTP Replay
  ↓
Normalized Result
```

也就是说：

- Storyboard 仍然只生成一次
- 图片仍然按 Scene 独立生成
- Web Provider 失败只影响当前 Step
- 产物仍按统一协议落盘
- 后续可以替换为官方 API 或本地模型

## 8. API 设计

### 8.1 创建项目

```http
POST /api/projects
Content-Type: application/json
```

请求：

```json
{
  "title": "小云朵的星星收集之旅",
  "story_text": "在很远很远的天空上，住着一朵软乎乎的小云朵...",
  "language": "zh-CN",
  "target_age": "4-6",
  "theme": "bedtime_story",
  "style": "children_storybook",
  "target_duration_sec": 600,
  "aspect_ratio": "16:9",
  "resolution": "1920x1080",
  "voice_name": "zh-CN-XiaoyiNeural"
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "status": "CREATED",
  "created_at": "2026-04-22T10:00:00+08:00"
}
```

### 8.2 启动生成

```http
POST /api/projects/{project_id}/start
Content-Type: application/json
```

返回：

```json
{
  "project_id": "pv_0001",
  "status": "STORYBOARD_GENERATING",
  "message": "generation started"
}
```

### 8.3 查询项目详情

```http
GET /api/projects/{project_id}
```

返回：

```json
{
  "project_id": "pv_0001",
  "title": "小云朵的星星收集之旅",
  "status": "SCENES_COMPOSING",
  "progress": 0.72,
  "current_step": "compose_scene_video",
  "scene_total": 12,
  "scene_done": 8,
  "final_video_url": null,
  "created_at": "2026-04-22T10:00:00+08:00",
  "updated_at": "2026-04-22T10:12:00+08:00"
}
```

### 8.4 查询 Storyboard

```http
GET /api/projects/{project_id}/storyboard
```

返回：

```json
{
  "project_id": "pv_0001",
  "storyboard": {
    "project": {},
    "global_style": {},
    "character_bible": {},
    "audio_profile": {},
    "video_profile": {},
    "render_rules": {},
    "scenes": []
  }
}
```

### 8.5 查询 Scene 列表

```http
GET /api/projects/{project_id}/scenes
```

返回：

```json
{
  "project_id": "pv_0001",
  "scenes": [
    {
      "scene_id": "scene_001",
      "sequence": 1,
      "title": "天空上的棉棉",
      "status": "DONE",
      "audio_duration_ms": 7420,
      "scene_video_url": "https://cdn.example.com/pv_0001/scenes/scene_001.mp4"
    }
  ]
}
```

### 8.6 查询 Scene 详情

```http
GET /api/projects/{project_id}/scenes/{scene_id}
```

返回：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_001",
  "sequence": 1,
  "title": "天空上的棉棉",
  "status": "DONE",
  "narration": "在很远很远的天空上，住着一朵软乎乎的小云朵，名字叫棉棉。",
  "image_url": "https://cdn.example.com/pv_0001/images/scene_001.png",
  "audio_url": "https://cdn.example.com/pv_0001/audio/scene_001.mp3",
  "subtitle_url": "https://cdn.example.com/pv_0001/subtitles/scene_001.srt",
  "scene_video_url": "https://cdn.example.com/pv_0001/scenes/scene_001.mp4"
}
```

### 8.7 重试项目

```http
POST /api/projects/{project_id}/retry
Content-Type: application/json
```

请求：

```json
{
  "from_step": "FINAL_COMPOSING"
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "status": "FINAL_COMPOSING",
  "message": "project retry submitted"
}
```

### 8.8 重试 Scene

```http
POST /api/projects/{project_id}/scenes/{scene_id}/retry
Content-Type: application/json
```

请求：

```json
{
  "step": "IMAGE_GENERATING"
}
```

返回：

```json
{
  "project_id": "pv_0001",
  "scene_id": "scene_003",
  "status": "PENDING",
  "message": "scene retry submitted"
}
```

### 8.9 查询资源列表

```http
GET /api/projects/{project_id}/assets
```

返回：

```json
{
  "project_id": "pv_0001",
  "assets": {
    "storyboard": "https://cdn.example.com/pv_0001/storyboard.json",
    "images": [],
    "audio": [],
    "subtitles": [],
    "scene_videos": [],
    "final_video": "https://cdn.example.com/pv_0001/final/final_video.mp4"
  }
}
```

### 8.10 获取最终结果

```http
GET /api/projects/{project_id}/result
```

返回：

```json
{
  "project_id": "pv_0001",
  "status": "DONE",
  "final_video_url": "https://cdn.example.com/pv_0001/final/final_video.mp4",
  "duration_ms": 598000
}
```

### 8.11 取消项目

```http
POST /api/projects/{project_id}/cancel
```

返回：

```json
{
  "project_id": "pv_0001",
  "status": "CANCELED"
}
```

### 8.12 查询任务日志

```http
GET /api/projects/{project_id}/tasks
```

返回：

```json
{
  "project_id": "pv_0001",
  "tasks": [
    {
      "task_id": "task_001",
      "scene_id": "scene_001",
      "task_type": "IMAGE_GENERATING",
      "status": "SUCCESS",
      "retry_count": 0,
      "error_code": null,
      "error_message": null
    }
  ]
}
```

## 9. 数据库设计

### 9.1 表关系概览

```text
projects 1 ─── 1 project_storyboards
projects 1 ─── N scenes
projects 1 ─── N tasks
scenes   1 ─── 1 scene_assets
tasks    1 ─── N task_logs
```

### 9.2 projects

```sql
CREATE TABLE projects (
  project_id VARCHAR(64) PRIMARY KEY,
  title VARCHAR(255) NOT NULL,
  status VARCHAR(64) NOT NULL,
  language VARCHAR(32) NOT NULL DEFAULT 'zh-CN',
  target_age VARCHAR(32),
  theme VARCHAR(64),
  tone VARCHAR(255),
  target_duration_sec INT,
  aspect_ratio VARCHAR(16) NOT NULL DEFAULT '16:9',
  resolution VARCHAR(32) NOT NULL DEFAULT '1920x1080',
  storyboard_path TEXT,
  final_video_path TEXT,
  final_video_url TEXT,
  progress DECIMAL(5, 4) NOT NULL DEFAULT 0,
  error_code VARCHAR(128),
  error_message TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_projects_status ON projects(status);
CREATE INDEX idx_projects_created_at ON projects(created_at);
```

### 9.3 project_storyboards

```sql
CREATE TABLE project_storyboards (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  project_id VARCHAR(64) NOT NULL,
  storyboard_path TEXT NOT NULL,
  storyboard_json JSON NOT NULL,
  schema_version VARCHAR(32) NOT NULL DEFAULT 'v1',
  validation_status VARCHAR(64) NOT NULL DEFAULT 'PENDING',
  validation_errors JSON,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_project_storyboards_project_id (project_id),
  CONSTRAINT fk_project_storyboards_project
    FOREIGN KEY (project_id) REFERENCES projects(project_id)
);
```

### 9.4 scenes

```sql
CREATE TABLE scenes (
  scene_id VARCHAR(64) PRIMARY KEY,
  project_id VARCHAR(64) NOT NULL,
  sequence INT NOT NULL,
  title VARCHAR(255),
  story_function VARCHAR(64),
  status VARCHAR(64) NOT NULL DEFAULT 'PENDING',
  narration TEXT NOT NULL,
  subtitle TEXT,
  duration_hint_sec INT,
  audio_duration_ms INT,
  scene_duration_ms INT,
  camera_motion VARCHAR(64),
  image_prompt_core TEXT,
  full_prompt TEXT,
  negative_prompt TEXT,
  scene_video_path TEXT,
  scene_video_url TEXT,
  error_code VARCHAR(128),
  error_message TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_scenes_project
    FOREIGN KEY (project_id) REFERENCES projects(project_id)
);

CREATE UNIQUE INDEX uk_scenes_project_sequence ON scenes(project_id, sequence);
CREATE INDEX idx_scenes_project_status ON scenes(project_id, status);
```

### 9.5 scene_assets

```sql
CREATE TABLE scene_assets (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  project_id VARCHAR(64) NOT NULL,
  scene_id VARCHAR(64) NOT NULL,
  image_path TEXT,
  image_url TEXT,
  audio_path TEXT,
  audio_url TEXT,
  subtitle_path TEXT,
  subtitle_url TEXT,
  scene_video_path TEXT,
  scene_video_url TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_scene_assets_scene_id (scene_id),
  CONSTRAINT fk_scene_assets_project
    FOREIGN KEY (project_id) REFERENCES projects(project_id),
  CONSTRAINT fk_scene_assets_scene
    FOREIGN KEY (scene_id) REFERENCES scenes(scene_id)
);

CREATE INDEX idx_scene_assets_project_id ON scene_assets(project_id);
```

### 9.6 tasks

```sql
CREATE TABLE tasks (
  task_id VARCHAR(64) PRIMARY KEY,
  project_id VARCHAR(64) NOT NULL,
  scene_id VARCHAR(64),
  task_type VARCHAR(64) NOT NULL,
  status VARCHAR(64) NOT NULL DEFAULT 'PENDING',
  retry_count INT NOT NULL DEFAULT 0,
  max_retry_count INT NOT NULL DEFAULT 3,
  locked_by VARCHAR(128),
  locked_at TIMESTAMP NULL,
  started_at TIMESTAMP NULL,
  finished_at TIMESTAMP NULL,
  error_code VARCHAR(128),
  error_message TEXT,
  payload JSON,
  result JSON,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_tasks_project
    FOREIGN KEY (project_id) REFERENCES projects(project_id)
);

CREATE INDEX idx_tasks_status_type ON tasks(status, task_type);
CREATE INDEX idx_tasks_project_scene ON tasks(project_id, scene_id);
CREATE INDEX idx_tasks_locked_at ON tasks(locked_at);
```

### 9.7 task_logs

```sql
CREATE TABLE task_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  project_id VARCHAR(64) NOT NULL,
  scene_id VARCHAR(64),
  level VARCHAR(32) NOT NULL DEFAULT 'INFO',
  event VARCHAR(128) NOT NULL,
  message TEXT,
  context JSON,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_task_logs_task
    FOREIGN KEY (task_id) REFERENCES tasks(task_id)
);

CREATE INDEX idx_task_logs_task_id ON task_logs(task_id);
CREATE INDEX idx_task_logs_project_id ON task_logs(project_id);
CREATE INDEX idx_task_logs_created_at ON task_logs(created_at);
```

## 10. 状态机设计

### 10.1 Project 状态机

```text
CREATED
→ STORYBOARD_GENERATING
→ STORYBOARD_READY
→ STORYBOARD_VALIDATED
→ IMAGES_GENERATING
→ IMAGES_READY
→ TTS_GENERATING
→ AUDIO_READY
→ SUBTITLES_READY
→ SCENES_COMPOSING
→ SCENES_READY
→ FINAL_COMPOSING
→ FINAL_READY
→ UPLOADING
→ DONE
```

失败分支：

- `FAILED_RETRYABLE`
- `FAILED_NEEDS_HUMAN`
- `CANCELED`

### 10.2 Scene 状态机

```text
PENDING
→ IMAGE_READY
→ AUDIO_READY
→ SUBTITLE_READY
→ SCENE_VIDEO_READY
→ DONE
```

失败分支：

- `IMAGE_FAILED`
- `TTS_FAILED`
- `SUBTITLE_FAILED`
- `COMPOSE_FAILED`

### 10.3 Task 状态机

```text
PENDING
→ RUNNING
→ SUCCESS
```

失败分支：

- `FAILED_RETRYABLE`
- `FAILED_FINAL`
- `CANCELED`

## 11. Prompt 拼接协议设计

### 11.1 设计原则

不要让 LLM 直接输出 `full_prompt`。LLM 只输出结构化 Prompt 片段，后端按模板稳定拼接。

### 11.2 拼接公式

```text
full_prompt
= character_anchors
+ object_anchors
+ subject_prompt
+ scene_prompt
+ environment
+ style
+ composition
+ quality_tags
```

### 11.3 示例

```text
a fluffy white cloud character named Mianmian, soft rounded shape, cute small eyes, warm smile,
tiny glowing stars with cute magical sparkle,
a fluffy white cloud character named Mianmian floating in the dreamy night sky,
soft stars scattered across the sky, calm bedtime atmosphere,
location: night sky,
time: night,
atmosphere: dreamy and peaceful,
composition: medium wide shot,
style: children storybook illustration,
render style: soft 2D animated illustration,
lighting: soft dreamy night lighting,
color palette: pastel blue, warm yellow, soft pink, lavender,
mood: calm, cozy, magical, bedtime,
clean composition, consistent character design, gentle expressions
```

### 11.4 negative_prompt 规则

```text
negative_prompt
= global_style.negative_prompt
+ scene.prompt.negative_prompt_override
+ platform_default_negative_prompt
```

## 12. 失败重试设计

### 12.1 原则

只重试失败节点，不重跑整单。

### 12.2 支持的重试粒度

- Project 级
- Scene 级
- Step 级

### 12.3 重试示例

- 第 3 个 Scene 出图失败，只重试 `render_image(scene_003)`
- 第 7 个 Scene TTS 失败，只重试 `generate_tts(scene_007)`
- final 合成失败，只重试 `compose_final_video`

### 12.4 幂等设计

每个 Step 执行前先检查目标产物是否已存在：

- 图片已存在则跳过 Image Worker
- 音频已存在则跳过 TTS Worker
- 字幕已存在则跳过 Subtitle Builder
- Scene 视频已存在则跳过 Scene Compose
- final 视频已存在则跳过 Final Compose

## 13. 目录结构设计

### 13.1 单仓库目录结构

```text
project_root/
├── backend/
│   ├── api/
│   │   ├── handlers/
│   │   ├── middlewares/
│   │   └── routes/
│   ├── orchestrator/
│   │   ├── state_machine/
│   │   ├── scheduler/
│   │   └── retry/
│   ├── services/
│   │   ├── llm/
│   │   ├── storyboard/
│   │   ├── storage/
│   │   └── notification/
│   ├── models/
│   ├── repository/
│   ├── config/
│   └── main.go
│
├── workers/
│   ├── image_worker/
│   │   ├── app/
│   │   ├── clients/
│   │   └── main.py
│   ├── tts_worker/
│   │   ├── app/
│   │   ├── subtitle/
│   │   └── main.py
│   └── ffmpeg_worker/
│       ├── app/
│       ├── filters/
│       ├── templates/
│       └── main.py
│
├── shared/
│   ├── schemas/
│   │   ├── storyboard.schema.json
│   │   ├── image_worker.schema.json
│   │   ├── tts_worker.schema.json
│   │   └── ffmpeg_worker.schema.json
│   ├── constants/
│   └── utils/
│
├── storage/
│   ├── projects/
│   │   └── pv_0001/
│   │       ├── storyboard.json
│   │       ├── images/
│   │       ├── audio/
│   │       ├── subtitles/
│   │       ├── scenes/
│   │       └── final/
│   └── bgm/
│
├── migrations/
│   └── 001_init.sql
│
├── scripts/
│   ├── dev_start.sh
│   ├── compose_scene.sh
│   └── compose_final.sh
│
├── docs/
│   ├── child_story_video_system_no_openclaw.md
│   ├── api.md
│   └── storyboard_schema.md
│
├── deploy/
│   ├── docker-compose.yml
│   ├── backend.Dockerfile
│   └── worker.Dockerfile
│
└── README.md
```

### 13.2 项目产物目录结构

```text
storage/projects/{project_id}/
├── storyboard.json
├── meta.json
├── images/
│   ├── scene_001.png
│   └── scene_002.png
├── audio/
│   ├── scene_001.mp3
│   └── scene_002.mp3
├── subtitles/
│   ├── scene_001.srt
│   └── scene_002.srt
├── scenes/
│   ├── scene_001.mp4
│   └── scene_002.mp4
├── final/
│   └── final_video.mp4
└── logs/
    ├── project.log
    └── ffmpeg.log
```

## 14. 服务拆分设计

### 14.1 Backend API Service

职责：

- 提供 HTTP API
- 鉴权与参数校验
- 创建项目
- 查询项目、Scene、Task、资源
- 提交重试、取消、下载等操作

部署建议：

- 可多实例部署
- 无状态服务
- 通过 DB 和 Queue 协调任务

### 14.2 Orchestrator Service

职责：

- 维护项目状态机
- 创建任务
- 调度 Worker
- 聚合任务结果
- 控制失败重试

部署建议：

- 首版可和 Backend API 放在同一进程
- 后续可独立为常驻调度服务

### 14.3 LLM Service

职责：

- 封装 LLM API
- 生成 `storyboard.json`
- 执行 JSON 输出约束
- 保存原始请求与模型响应日志

边界：

- 只参与 Storyboard 生成
- 不参与后续 Worker 决策

### 14.4 Image Worker Service

职责：

- 从任务队列领取图片生成任务
- 调用图像模型
- 写入图片文件
- 更新 `scene_assets`

部署建议：

- 可水平扩展
- 适合独立限流
- 可按图像模型供应商做适配层

### 14.5 TTS Worker Service

职责：

- 从任务队列领取 TTS 任务
- 调用 TTS 服务
- 获取音频时长
- 生成字幕时间轴
- 输出音频与 `.srt`

部署建议：

- 可水平扩展
- 需要处理 TTS 服务限流与失败重试

### 14.6 FFmpeg Worker Service

职责：

- Scene 视频合成
- final 视频拼接
- 转场生成
- 字幕烧录
- BGM 混音

部署建议：

- CPU 密集型
- 和 Image / TTS Worker 分开部署
- 可限制并发，避免机器负载过高

### 14.7 Storage Service

职责：

- 抽象本地文件系统、OSS、S3
- 统一上传、下载、签名 URL
- 管理中间产物生命周期

### 14.8 Queue / Task Service

职责：

- 管理待执行任务
- 支持 Worker 抢占锁
- 支持超时恢复
- 支持失败重试

首版建议：

- 使用 DB 任务表 + 定时扫描

后续升级：

- Redis + Celery
- Redis + BullMQ
- Kafka / RabbitMQ

### 14.9 Notification Service

职责：

- 项目完成通知
- 项目失败通知
- 后台告警

通知方式：

- WebSocket
- 站内通知
- 邮件
- IM Webhook

## 15. 技术选型建议

### 15.1 后端

推荐组合：

- Go：适合 API、Orchestrator、任务调度、状态机
- Python：适合 Worker、媒体处理、AI 工具链

建议：

- 如果更重工程稳定，使用 Go 做 API / Orchestrator
- Worker 使用 Python，方便接入图像模型、TTS 和 FFmpeg 工具链

### 15.2 队列

可选方案：

- DB 任务表 + 定时扫描
- Redis + Celery
- Redis + BullMQ
- RabbitMQ

首版建议：

- 先用 DB 任务表 + 定时扫描
- 后续任务规模上来后再切 Redis 队列

### 15.3 存储

开发环境：

- 本地磁盘

生产环境：

- OSS
- S3
- MinIO

### 15.4 视频处理

FFmpeg 必选。

首版不建议引入过重的视频编辑框架，优先使用可控的 FFmpeg Filter Graph 和固定模板实现。

## 16. MVP 范围建议

### 16.1 第一阶段

先做：

- 输入故事文本
- 一次 LLM 生成 `storyboard.json`
- JSON Schema 校验
- Scene 级图片生成
- Scene 级 TTS + 字幕
- FFmpeg 合成 Scene 视频
- final video 导出
- 项目状态页
- Scene 重试

### 16.2 第一阶段先不做

- 复杂在线编辑器
- 多人协作
- 多角色配音
- 视频模型直出长视频
- 多渠道自动分发
- 高级模板市场

### 16.3 第二阶段

可扩展：

- 多语言生成
- 多模板系统
- 用户自定义角色
- 字幕样式编辑
- BGM 库管理
- 批量生成
- 运营后台

## 17. 最终结论

这套无 OpenClaw 的儿童故事视频生成系统，本质上是：

```text
一次性模型理解 + 工程化媒体生产流水线
```

它的优点非常明确：

- 更轻
- 更稳
- 更容易调试
- 更适合 MVP
- 更适合 Scene 级失败重试
- 更适合产品化落地

系统关键设计点只有两个：

1. LLM 只生成一次 `storyboard.json`
2. 后续全部交给规则化 Worker 执行

这样构建出来的是一个真正的媒体生产后端系统，而不是一个链路不可控、调试困难的 Agent 系统。
