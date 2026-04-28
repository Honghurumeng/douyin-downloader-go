import { Download, LoaderCircle, Play, Plus, Star, Tag, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState, type ChangeEvent } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

type TagRecord = {
  id: number;
  name: string;
  createdAt: string;
  videoCount: number;
};

type VideoRecord = {
  id: string;
  videoId: string;
  title: string;
  description: string;
  author: string;
  authorId: string;
  shareUrl: string;
  originalUrl: string;
  contentType: string;
  coverUrl: string;
  coverSourceUrl: string;
  coverLocalFile: string;
  coverLocalUrl: string;
  videoUri: string;
  downloadUrl: string;
  watermarkUrl: string;
  videoWidth: number;
  videoHeight: number;
  duration: number;
  likeCount: number;
  commentCount: number;
  shareCount: number;
  collectCount: number;
  rating: number;
  localFile: string;
  localUrl: string;
  fileSize: number;
  savedAt: string;
  updatedAt: string;
  lastSourceInput: string;
  tags: TagRecord[];
};

type ListResponse = {
  videos: VideoRecord[];
};

type TagListResponse = {
  tags: TagRecord[];
};

type DownloadResponse = {
  video: VideoRecord;
  alreadyStored: boolean;
  downloadedNow: boolean;
};

type RatingResponse = {
  video: VideoRecord;
};

type TagMutationResponse = {
  tag: TagRecord;
};

type DeleteResponse = {
  deleted: boolean;
  videoId?: string;
  tagId?: number;
};

type ErrorResponse = {
  error: string;
};

type RatingFilter = "all" | "rated" | "unrated" | "1" | "2" | "3" | "4" | "5";

const initialShareText =
  "1.02 mDh:/ 09/16 O@x.Sl 回复 @cfc的评论 加长版来咯# 捣蒜舞 # 捣蒜舞挑战 # 捣蒜舞加长版 # 原声 # 加长版 https://v.douyin.com/h5H9gfOg0iM/ 复制此链接，打开Dou音搜索，直接观看视频！";

const filterOptions: Array<{ label: string; value: RatingFilter }> = [
  { label: "全部", value: "all" },
  { label: "已评分", value: "rated" },
  { label: "未评分", value: "unrated" },
  { label: "5 分", value: "5" },
  { label: "4 分", value: "4" },
  { label: "3 分", value: "3" },
  { label: "2 分", value: "2" },
  { label: "1 分", value: "1" },
];

export default function App() {
  const [shareText, setShareText] = useState(initialShareText);
  const [videos, setVideos] = useState<VideoRecord[]>([]);
  const [tags, setTags] = useState<TagRecord[]>([]);
  const [selectedVideo, setSelectedVideo] = useState<VideoRecord | null>(null);
  const [ratingFilter, setRatingFilter] = useState<RatingFilter>("all");
  const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);
  const [newTagName, setNewTagName] = useState("");
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [creatingTag, setCreatingTag] = useState(false);
  const [ratingVideoId, setRatingVideoId] = useState<string | null>(null);
  const [taggingVideoId, setTaggingVideoId] = useState<string | null>(null);
  const [deletingVideoId, setDeletingVideoId] = useState<string | null>(null);
  const [deletingTagId, setDeletingTagId] = useState<number | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    void loadTags();
  }, []);

  useEffect(() => {
    void loadVideos(ratingFilter, selectedTagIds);
  }, [ratingFilter, selectedTagIds]);

  const stats = useMemo(() => {
    const rated = videos.filter((video) => video.rating > 0).length;
    const totalDurationSeconds = videos.reduce((sum, video) => sum + video.duration, 0);
    return {
      total: videos.length,
      rated,
      totalDurationMinutes: Math.round((totalDurationSeconds / 60) * 10) / 10,
    };
  }, [videos]);

  async function loadTags() {
    try {
      const response = await fetch(withBasePath("/api/tags"));
      if (!response.ok) {
        throw new Error("加载标签失败");
      }

      const data = (await response.json()) as TagListResponse;
      setTags(data.tags);
      setSelectedVideo((current) =>
        current
          ? {
              ...current,
              tags: current.tags.filter((tag) => data.tags.some((availableTag) => availableTag.id === tag.id)),
            }
          : null,
      );
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "加载标签失败");
    }
  }

  async function loadVideos(filter: RatingFilter = ratingFilter, tagIDs: number[] = selectedTagIds) {
    setLoading(true);
    setError("");

    try {
      const search = new URLSearchParams();
      if (filter !== "all") {
        search.set("rating", filter);
      }
      if (tagIDs.length > 0) {
        search.set("tags", tagIDs.join(","));
      }

      const response = await fetch(withBasePath(`/api/videos${search.size > 0 ? `?${search.toString()}` : ""}`));
      if (!response.ok) {
        throw new Error("加载视频列表失败");
      }

      const data = (await response.json()) as ListResponse;
      setVideos(data.videos);
      setSelectedVideo((current) =>
        current ? data.videos.find((video) => video.videoId === current.videoId) ?? current : null,
      );
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "加载视频列表失败");
    } finally {
      setLoading(false);
    }
  }

  async function handleDownload() {
    if (!shareText.trim()) {
      setError("请输入分享文案或分享链接");
      return;
    }

    setSubmitting(true);
    setError("");
    setMessage("");

    try {
      const response = await fetch(withBasePath("/api/videos"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ shareText }),
      });

      const data = (await response.json()) as DownloadResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "下载失败");
      }

      const payload = data as DownloadResponse;
      setMessage(payload.alreadyStored ? "该视频已存在，本次直接复用本地文件。" : "视频已解析并保存到本地。");
      setShareText("");
      await Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()]);
      setSelectedVideo(payload.video);
    } catch (downloadError) {
      setError(downloadError instanceof Error ? downloadError.message : "下载失败");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleRate(videoId: string, rating: number) {
    setRatingVideoId(videoId);
    setError("");

    try {
      const response = await fetch(withBasePath(`/api/videos/${videoId}/rating`), {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ rating }),
      });

      const data = (await response.json()) as RatingResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "评分保存失败");
      }

      const payload = data as RatingResponse;
      await loadVideos(ratingFilter, selectedTagIds);
      setSelectedVideo(payload.video);
    } catch (ratingError) {
      setError(ratingError instanceof Error ? ratingError.message : "评分保存失败");
    } finally {
      setRatingVideoId(null);
    }
  }

  async function handleToggleVideoTag(video: VideoRecord, tagId: number) {
    const currentTagIds = video.tags.map((tag) => tag.id);
    const nextTagIds = currentTagIds.includes(tagId)
      ? currentTagIds.filter((id) => id !== tagId)
      : [...currentTagIds, tagId];

    setTaggingVideoId(video.videoId);
    setError("");

    try {
      const response = await fetch(withBasePath(`/api/videos/${video.videoId}/tags`), {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ tagIds: nextTagIds }),
      });

      const data = (await response.json()) as RatingResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "更新标签失败");
      }

      const payload = data as RatingResponse;
      await Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()]);
      setSelectedVideo(payload.video);
    } catch (tagError) {
      setError(tagError instanceof Error ? tagError.message : "更新标签失败");
    } finally {
      setTaggingVideoId(null);
    }
  }

  async function handleCreateTag() {
    if (!newTagName.trim()) {
      setError("请输入标签名称");
      return;
    }

    setCreatingTag(true);
    setError("");
    setMessage("");

    try {
      const response = await fetch(withBasePath("/api/tags"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ name: newTagName }),
      });

      const data = (await response.json()) as TagMutationResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "创建标签失败");
      }

      setMessage(`标签“${(data as TagMutationResponse).tag.name}”已创建。`);
      setNewTagName("");
      await loadTags();
    } catch (tagError) {
      setError(tagError instanceof Error ? tagError.message : "创建标签失败");
    } finally {
      setCreatingTag(false);
    }
  }

  async function handleDeleteTag(tag: TagRecord) {
    const confirmed = window.confirm(`确认删除标签“${tag.name}”吗？这会移除所有视频上的该标签。`);
    if (!confirmed) {
      return;
    }

    setDeletingTagId(tag.id);
    setError("");
    setMessage("");

    const nextSelectedTagIds = selectedTagIds.filter((id) => id !== tag.id);

    try {
      const response = await fetch(withBasePath(`/api/tags/${tag.id}`), {
        method: "DELETE",
      });

      const data = (await response.json()) as DeleteResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "删除标签失败");
      }

      setSelectedTagIds(nextSelectedTagIds);
      setSelectedVideo((current) =>
        current
          ? {
              ...current,
              tags: current.tags.filter((videoTag) => videoTag.id !== tag.id),
            }
          : null,
      );
      setMessage(`标签“${tag.name}”已删除。`);
      await Promise.all([loadTags(), loadVideos(ratingFilter, nextSelectedTagIds)]);
    } catch (tagError) {
      setError(tagError instanceof Error ? tagError.message : "删除标签失败");
    } finally {
      setDeletingTagId(null);
    }
  }

  async function handleDelete(video: VideoRecord) {
    const confirmed = window.confirm(`确认删除《${video.title}》的本地文件和记录吗？`);
    if (!confirmed) {
      return;
    }

    setDeletingVideoId(video.videoId);
    setError("");
    setMessage("");

    try {
      const response = await fetch(withBasePath(`/api/videos/${video.videoId}`), {
        method: "DELETE",
      });

      const data = (await response.json()) as DeleteResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "删除失败");
      }

      setMessage("视频文件和本地记录已删除。");
      setSelectedVideo((current) => (current?.videoId === video.videoId ? null : current));
      await Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()]);
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "删除失败");
    } finally {
      setDeletingVideoId(null);
    }
  }

  function toggleTagFilter(tagId: number) {
    setSelectedTagIds((current) =>
      current.includes(tagId) ? current.filter((id) => id !== tagId) : [...current, tagId],
    );
  }

  return (
    <div className="min-h-screen bg-[color:var(--canvas)] text-[color:var(--foreground)]">
      <div className="absolute inset-0 -z-10 bg-[radial-gradient(circle_at_top_left,_rgba(37,99,235,0.07),_transparent_28%),linear-gradient(to_bottom,_rgba(255,255,255,0.75),_rgba(255,255,255,0.95))]" />
      <div className="absolute inset-0 -z-10 opacity-60 [background-image:linear-gradient(rgba(148,163,184,0.12)_1px,transparent_1px),linear-gradient(90deg,rgba(148,163,184,0.12)_1px,transparent_1px)] [background-size:24px_24px]" />

      <div className="mx-auto flex min-h-screen w-full max-w-7xl flex-col px-4 py-6 sm:px-6 lg:px-8">
        <header className="flex flex-col gap-4 border-b border-[color:var(--border)] pb-6 md:flex-row md:items-end md:justify-between">
          <div className="max-w-2xl">
            <p className="text-sm font-medium text-[color:var(--primary)]">本地视频工作台</p>
            <h1 className="mt-2 text-3xl font-semibold tracking-tight">抖音分享链接下载、评分与标签</h1>
            <p className="mt-3 text-sm leading-6 text-[color:var(--foreground-muted)]">
              输入分享文案或短链，服务端会用纯 Go 解析抖音页面、下载视频和封面到本地，并把评分、标签和文件信息保存在 SQLite 中。
            </p>
          </div>

          <div className="grid grid-cols-3 gap-3 md:min-w-[360px]">
            <StatCard label="当前结果" value={String(stats.total)} />
            <StatCard label="已评分" value={String(stats.rated)} />
            <StatCard label="总时长" value={`${stats.totalDurationMinutes} 分钟`} />
          </div>
        </header>

        <main className="mt-6 grid flex-1 gap-6 xl:grid-cols-[380px_minmax(0,1fr)]">
          <div className="space-y-4">
            <Card className="h-fit">
              <CardHeader>
                <CardTitle>获取视频</CardTitle>
                <CardDescription>支持直接粘贴整段分享文案，后端会自动提取其中的抖音短链。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="share-text">分享内容</Label>
                  <Textarea
                    id="share-text"
                    value={shareText}
                    onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setShareText(event.target.value)}
                    placeholder="粘贴抖音分享链接或整段分享文案"
                  />
                </div>

                <div className="rounded-lg border border-dashed border-[color:var(--border)] bg-[color:var(--surface-subtle)] px-3 py-3 text-sm text-[color:var(--foreground-muted)]">
                  数据会保存到当前二进制所在目录下的 <code className="font-mono text-[13px] text-[color:var(--foreground)]">data/videos/</code>、
                  <code className="font-mono text-[13px] text-[color:var(--foreground)]">data/covers/</code> 和{" "}
                  <code className="font-mono text-[13px] text-[color:var(--foreground)]">data/videos.db</code>。
                </div>

                <Button className="w-full" size="lg" onClick={handleDownload} disabled={submitting}>
                  {submitting ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
                  获取视频并保存到本地
                </Button>

                {message ? (
                  <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-3 text-sm text-emerald-800">
                    {message}
                  </div>
                ) : null}

                {error ? (
                  <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-3 text-sm text-rose-700">
                    {error}
                  </div>
                ) : null}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>标签管理</CardTitle>
                <CardDescription>可创建、删除标签。一个视频可以绑定多个标签，筛选时会按所选标签的交集返回。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    value={newTagName}
                    onChange={(event: ChangeEvent<HTMLInputElement>) => setNewTagName(event.target.value)}
                    placeholder="例如：舞蹈、素材、复盘"
                  />
                  <Button className="shrink-0" onClick={() => void handleCreateTag()} disabled={creatingTag}>
                    {creatingTag ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
                    创建
                  </Button>
                </div>

                <div className="flex flex-wrap gap-2">
                  {tags.map((tag) => {
                    const selected = selectedTagIds.includes(tag.id);
                    return (
                      <div
                        key={tag.id}
                        className={cn(
                          "flex items-center overflow-hidden rounded-full border bg-white",
                          selected ? "border-[color:var(--primary)]" : "border-[color:var(--border)]",
                        )}
                      >
                        <button
                          type="button"
                          onClick={() => toggleTagFilter(tag.id)}
                          className={cn(
                            "flex items-center gap-2 px-3 py-1.5 text-sm transition-colors",
                            selected
                              ? "bg-[color:var(--surface-accent)] text-[color:var(--primary)]"
                              : "text-[color:var(--foreground)] hover:bg-[color:var(--surface-subtle)]",
                          )}
                        >
                          <span>{tag.name}</span>
                          <span className="text-xs text-[color:var(--foreground-soft)]">{tag.videoCount}</span>
                        </button>
                        <button
                          type="button"
                          onClick={() => void handleDeleteTag(tag)}
                          disabled={deletingTagId === tag.id}
                          className="border-l border-[color:var(--border)] px-2 py-1.5 text-[color:var(--foreground-soft)] transition-colors hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50"
                        >
                          {deletingTagId === tag.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <X className="h-3.5 w-3.5" />}
                        </button>
                      </div>
                    );
                  })}
                </div>

                {tags.length === 0 ? (
                  <div className="rounded-lg border border-[color:var(--border)] bg-[color:var(--surface-subtle)] px-3 py-3 text-sm text-[color:var(--foreground-muted)]">
                    还没有标签。先创建几个常用标签，后面可以在视频详情里多选绑定。
                  </div>
                ) : (
                  <div className="text-xs leading-5 text-[color:var(--foreground-soft)]">
                    点击标签名称可加入右侧筛选；点击右侧关闭按钮会删除该标签及其在所有视频上的绑定关系。
                  </div>
                )}
              </CardContent>
            </Card>
          </div>

          <section className="space-y-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 className="text-lg font-semibold">已保存的视频</h2>
                <p className="text-sm text-[color:var(--foreground-muted)]">支持按评分和多个标签交叉筛选，点击条目打开播放、评分、标签和删除窗口。</p>
              </div>
              <Button variant="secondary" onClick={() => void Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()])} disabled={loading}>
                {loading ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : null}
                刷新列表
              </Button>
            </div>

            <div className="space-y-3 rounded-xl border border-[color:var(--border)] bg-white p-4">
              <div className="space-y-2">
                <div className="text-xs font-medium uppercase tracking-[0.08em] text-[color:var(--foreground-soft)]">评分筛选</div>
                <div className="flex flex-wrap gap-2">
                  {filterOptions.map((option) => (
                    <Button
                      key={option.value}
                      variant={ratingFilter === option.value ? "default" : "secondary"}
                      size="sm"
                      onClick={() => setRatingFilter(option.value)}
                    >
                      {option.label}
                    </Button>
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-xs font-medium uppercase tracking-[0.08em] text-[color:var(--foreground-soft)]">标签筛选</div>
                  <div className="text-xs text-[color:var(--foreground-soft)]">多选时按交集过滤</div>
                </div>

                {tags.length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {tags.map((tag) => {
                      const selected = selectedTagIds.includes(tag.id);
                      return (
                        <Button
                          key={tag.id}
                          variant={selected ? "default" : "secondary"}
                          size="sm"
                          className="gap-2"
                          onClick={() => toggleTagFilter(tag.id)}
                        >
                          <Tag className="h-3.5 w-3.5" />
                          {tag.name}
                        </Button>
                      );
                    })}
                  </div>
                ) : (
                  <div className="text-sm text-[color:var(--foreground-muted)]">还没有标签，先在左侧创建后再筛选。</div>
                )}
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
              {videos.map((video) => (
                <button
                  key={video.videoId}
                  type="button"
                  onClick={() => setSelectedVideo(video)}
                  className="text-left"
                >
                  <Card className="h-full transition-colors hover:border-[color:var(--border-strong)]">
                    <div className="flex h-80 items-center justify-center overflow-hidden rounded-t-xl bg-[color:var(--surface-subtle)] p-3">
                      <img
                        src={video.coverUrl}
                        alt={video.title}
                        className="h-full w-full object-contain"
                        loading="lazy"
                      />
                    </div>
                    <CardContent className="space-y-4 pt-4">
                      <div className="flex flex-wrap items-center gap-2">
                        <Badge variant="default">{video.author}</Badge>
                        {video.rating > 0 ? (
                          <Badge variant="rating">评分 {video.rating}/5</Badge>
                        ) : (
                          <Badge variant="neutral">未评分</Badge>
                        )}
                      </div>

                      {video.tags.length > 0 ? (
                        <div className="flex flex-wrap gap-2">
                          {video.tags.map((tag) => (
                            <Badge key={tag.id} variant="neutral">
                              {tag.name}
                            </Badge>
                          ))}
                        </div>
                      ) : null}

                      <div>
                        <h3 className="line-clamp-2 text-sm font-semibold leading-6">{video.title}</h3>
                        <p className="mt-2 line-clamp-2 text-sm leading-6 text-[color:var(--foreground-muted)]">
                          {video.lastSourceInput}
                        </p>
                      </div>

                      <div className="flex items-center justify-between text-xs text-[color:var(--foreground-soft)]">
                        <span>{formatDuration(video.duration)}</span>
                        <span>{formatDate(video.savedAt)}</span>
                      </div>
                    </CardContent>
                  </Card>
                </button>
              ))}

              {!loading && videos.length === 0 ? (
                <Card className="md:col-span-2 2xl:col-span-3">
                  <CardContent className="flex min-h-56 flex-col items-center justify-center py-10 text-center">
                    <Play className="h-10 w-10 text-[color:var(--foreground-soft)]" />
                    <h3 className="mt-4 text-base font-semibold">当前筛选下没有视频</h3>
                    <p className="mt-2 max-w-md text-sm leading-6 text-[color:var(--foreground-muted)]">
                      你可以先在左侧保存一个抖音视频，或者调整上方评分与标签筛选条件查看其他内容。
                    </p>
                  </CardContent>
                </Card>
              ) : null}
            </div>
          </section>
        </main>
      </div>

      <Dialog open={Boolean(selectedVideo)} onOpenChange={(open: boolean) => !open && setSelectedVideo(null)}>
        {selectedVideo ? (
          <DialogContent className="max-h-[90vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>{selectedVideo.title}</DialogTitle>
              <DialogDescription>
                来自 {selectedVideo.author}，本地文件路径为{" "}
                <code className="font-mono text-[13px] text-[color:var(--foreground)]">{selectedVideo.localFile}</code>
              </DialogDescription>
            </DialogHeader>

            <div className="grid gap-6 lg:grid-cols-[minmax(0,1.4fr)_320px]">
              <div className="space-y-4">
                <div className="overflow-hidden rounded-xl border border-[color:var(--border)] bg-black p-2">
                  <video
                    src={selectedVideo.localUrl}
                    poster={selectedVideo.coverUrl}
                    controls
                    className="mx-auto block max-h-[72vh] w-auto max-w-full bg-black"
                  />
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <Metric label="时长" value={formatDuration(selectedVideo.duration)} />
                  <Metric label="尺寸" value={formatVideoResolution(selectedVideo.videoWidth, selectedVideo.videoHeight)} />
                  <Metric label="点赞" value={String(selectedVideo.likeCount)} />
                  <Metric label="评论" value={String(selectedVideo.commentCount)} />
                </div>
              </div>

              <div className="space-y-4">
                <Card className="bg-[color:var(--surface-subtle)]">
                  <CardHeader className="pb-4">
                    <CardTitle className="text-sm">评分</CardTitle>
                    <CardDescription>评分会和分享链接一起保存在 SQLite 中。</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    <div className="grid grid-cols-5 gap-2">
                      {[1, 2, 3, 4, 5].map((score) => (
                        <Button
                          key={score}
                          variant={selectedVideo.rating === score ? "default" : "secondary"}
                          className={cn("gap-1", ratingVideoId === selectedVideo.videoId && "opacity-70")}
                          onClick={() => void handleRate(selectedVideo.videoId, score)}
                          disabled={ratingVideoId === selectedVideo.videoId}
                        >
                          <Star className="h-3.5 w-3.5" />
                          {score}
                        </Button>
                      ))}
                    </div>
                    <p className="text-xs leading-5 text-[color:var(--foreground-muted)]">
                      当前评分：{selectedVideo.rating > 0 ? `${selectedVideo.rating}/5` : "未评分"}
                    </p>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader className="pb-4">
                    <CardTitle className="text-sm">标签</CardTitle>
                    <CardDescription>可为当前视频绑定多个标签。多选筛选时会按所选标签交集返回。</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {tags.length > 0 ? (
                      <div className="flex flex-wrap gap-2">
                        {tags.map((tag) => {
                          const selected = selectedVideo.tags.some((videoTag) => videoTag.id === tag.id);
                          return (
                            <Button
                              key={tag.id}
                              size="sm"
                              variant={selected ? "default" : "secondary"}
                              onClick={() => void handleToggleVideoTag(selectedVideo, tag.id)}
                              disabled={taggingVideoId === selectedVideo.videoId}
                            >
                              <Tag className="mr-1 h-3.5 w-3.5" />
                              {tag.name}
                            </Button>
                          );
                        })}
                      </div>
                    ) : (
                      <div className="text-sm text-[color:var(--foreground-muted)]">还没有标签，先在左侧创建后再给视频绑定。</div>
                    )}

                    <div className="rounded-lg border border-[color:var(--border)] bg-[color:var(--surface-subtle)] px-3 py-3 text-sm text-[color:var(--foreground-muted)]">
                      当前标签：
                      {selectedVideo.tags.length > 0
                        ? ` ${selectedVideo.tags.map((tag) => tag.name).join("、")}`
                        : " 未绑定标签"}
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader className="pb-4">
                    <CardTitle className="text-sm">来源信息</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-[color:var(--foreground-muted)]">
                    <InfoRow label="封面来源" value={selectedVideo.coverSourceUrl || selectedVideo.coverUrl} />
                    <InfoRow label="本地封面" value={selectedVideo.coverLocalFile || "未本地化"} />
                    <InfoRow label="分享短链" value={selectedVideo.shareUrl} />
                    <InfoRow label="下载地址" value={selectedVideo.downloadUrl} />
                    <InfoRow label="原始分享页" value={selectedVideo.originalUrl} />
                    <InfoRow label="本地大小" value={formatFileSize(selectedVideo.fileSize)} />
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader className="pb-4">
                    <CardTitle className="text-sm">删除视频</CardTitle>
                    <CardDescription>会同时删除本地 MP4 文件、封面文件和 SQLite 里的元数据记录。</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <Button
                      variant="destructive"
                      className="w-full"
                      onClick={() => void handleDelete(selectedVideo)}
                      disabled={deletingVideoId === selectedVideo.videoId}
                    >
                      {deletingVideoId === selectedVideo.videoId ? (
                        <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                      ) : (
                        <Trash2 className="mr-2 h-4 w-4" />
                      )}
                      删除本地文件和记录
                    </Button>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader className="pb-4">
                    <CardTitle className="text-sm">原始文案</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="rounded-lg border border-[color:var(--border)] bg-[color:var(--surface-subtle)] px-3 py-3 text-sm leading-6 text-[color:var(--foreground-muted)]">
                      {selectedVideo.lastSourceInput}
                    </div>
                  </CardContent>
                </Card>
              </div>
            </div>
          </DialogContent>
        ) : null}
      </Dialog>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-3">
      <div className="text-xs font-medium text-[color:var(--foreground-soft)]">{label}</div>
      <div className="mt-1 text-lg font-semibold tracking-tight">{value}</div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-3">
      <div className="text-xs font-medium text-[color:var(--foreground-soft)]">{label}</div>
      <div className="mt-2 text-base font-semibold">{value}</div>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-[color:var(--foreground-soft)]">{label}</div>
      <div className="break-all rounded-md bg-[color:var(--surface-subtle)] px-3 py-2 font-mono text-[12px] leading-5 text-[color:var(--foreground)]">
        {value}
      </div>
    </div>
  );
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatDuration(seconds: number) {
  const totalSeconds = Math.max(0, Math.round(seconds));
  const mins = Math.floor(totalSeconds / 60);
  const secs = totalSeconds % 60;
  return `${mins}:${String(secs).padStart(2, "0")}`;
}

function formatFileSize(bytes: number) {
  if (bytes <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  return `${value.toFixed(value >= 100 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function formatVideoResolution(width: number, height: number) {
  if (width > 0 && height > 0) {
    return `${width} × ${height}`;
  }

  return "待补全";
}

function withBasePath(value: string) {
  return `${getAppBasePath()}${normalizeAppPath(value)}`;
}

function getAppBasePath() {
  if (typeof document === "undefined") {
    return "";
  }

  const declaredBasePath = document
    .querySelector('meta[name="app-base-path"]')
    ?.getAttribute("content")
    ?.trim();

  if (!declaredBasePath || declaredBasePath === "__APP_BASE_PATH__" || declaredBasePath === "/") {
    return "";
  }

  return `/${declaredBasePath.replace(/^\/+|\/+$/g, "")}`;
}

function normalizeAppPath(value: string) {
  if (!value) {
    return "/";
  }

  return value.startsWith("/") ? value : `/${value}`;
}
