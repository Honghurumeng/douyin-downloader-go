import { ChevronLeft, ChevronRight, Download, LoaderCircle, LogOut, Play, Plus, Star, Tag, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState, type CSSProperties, type ChangeEvent } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { getLoginRoute, withBasePath } from "@/lib/app-paths";
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

type AuthSessionResponse = {
  enabled: boolean;
  authenticated: boolean;
};

type RatingFilter = "all" | "rated" | "unrated" | "1" | "2" | "3" | "4" | "5";

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

const runwayThemeStyle = {
  "--canvas": "#000000",
  "--surface-subtle": "#111111",
  "--surface-alt": "#151515",
  "--surface-accent": "#141414",
  "--foreground": "#ffffff",
  "--foreground-muted": "#767d88",
  "--foreground-soft": "#a7a7a7",
  "--primary": "#ffffff",
  "--primary-strong": "#e9ecf2",
  "--border": "#27272a",
  "--border-strong": "#3f3f46",
  "--ring": "rgba(255,255,255,0.18)",
} as CSSProperties;

export default function VideoLibraryApp() {
  const [authEnabled, setAuthEnabled] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [shareText, setShareText] = useState("");
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
    void loadSession();
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

  const selectedVideoIndex = useMemo(() => {
    if (!selectedVideo) {
      return -1;
    }

    return videos.findIndex((video) => video.videoId === selectedVideo.videoId);
  }, [selectedVideo, videos]);

  const featuredVideo = selectedVideo ?? videos[0] ?? null;
  const canSelectPreviousVideo = selectedVideoIndex > 0;
  const canSelectNextVideo = selectedVideoIndex >= 0 && selectedVideoIndex < videos.length - 1;

  function redirectToLogin() {
    window.location.replace(getLoginRoute());
  }

  async function requestJSON<T extends object>(
    input: string,
    init?: RequestInit,
    unauthorizedMessage = "登录状态已失效，请重新输入密码",
  ) {
    const response = await fetch(withBasePath(input), init);

    if (response.status === 401) {
      redirectToLogin();
      throw new Error(unauthorizedMessage);
    }

    const data = (await response.json()) as T | ErrorResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "请求失败");
    }

    return data as T;
  }

  async function loadSession() {
    try {
      const response = await fetch(withBasePath("/api/auth/session"));
      if (response.status === 401) {
        redirectToLogin();
        return;
      }
      if (!response.ok) {
        throw new Error("获取登录状态失败");
      }

      const data = (await response.json()) as AuthSessionResponse;
      if (!data.authenticated) {
        redirectToLogin();
        return;
      }

      setAuthEnabled(data.enabled);
    } catch (sessionError) {
      setError(sessionError instanceof Error ? sessionError.message : "获取登录状态失败");
    }
  }

  async function loadTags() {
    try {
      const data = await requestJSON<TagListResponse>("/api/tags");
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

      const data = await requestJSON<ListResponse>(`/api/videos${search.size > 0 ? `?${search.toString()}` : ""}`);
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
      const payload = await requestJSON<DownloadResponse>("/api/videos", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ shareText }),
      });

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
      const payload = await requestJSON<RatingResponse>(`/api/videos/${videoId}/rating`, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ rating }),
      });
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
      const payload = await requestJSON<RatingResponse>(`/api/videos/${video.videoId}/tags`, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ tagIds: nextTagIds }),
      });
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
      const data = await requestJSON<TagMutationResponse>("/api/tags", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ name: newTagName }),
      });

      setMessage(`标签“${data.tag.name}”已创建。`);
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
      await requestJSON<DeleteResponse>(`/api/tags/${tag.id}`, {
        method: "DELETE",
      });

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
      await requestJSON<DeleteResponse>(`/api/videos/${video.videoId}`, {
        method: "DELETE",
      });

      setMessage("视频文件和本地记录已删除。");
      setSelectedVideo((current) => (current?.videoId === video.videoId ? null : current));
      await Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()]);
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "删除失败");
    } finally {
      setDeletingVideoId(null);
    }
  }

  async function handleLogout() {
    setLoggingOut(true);
    setError("");
    setMessage("");

    try {
      await requestJSON<AuthSessionResponse>("/api/auth/logout", {
        method: "POST",
      });
    } catch (logoutError) {
      setError(logoutError instanceof Error ? logoutError.message : "退出登录失败");
    } finally {
      setLoggingOut(false);
      redirectToLogin();
    }
  }

  function toggleTagFilter(tagId: number) {
    setSelectedTagIds((current) =>
      current.includes(tagId) ? current.filter((id) => id !== tagId) : [...current, tagId],
    );
  }

  function selectAdjacentVideo(direction: -1 | 1) {
    if (selectedVideoIndex < 0) {
      return;
    }

    const nextVideo = videos[selectedVideoIndex + direction];
    if (!nextVideo) {
      return;
    }

    setSelectedVideo(nextVideo);
  }

  return (
    <div style={runwayThemeStyle} className="min-h-screen bg-[color:var(--canvas)] text-[color:var(--foreground)]">
      <div className="relative overflow-hidden border-b border-[#27272a]">
        {featuredVideo?.coverUrl ? (
          <img
            src={featuredVideo.coverUrl}
            alt={getDisplayTitle(featuredVideo)}
            className="absolute inset-0 h-full w-full object-cover opacity-34"
          />
        ) : null}
        <div className="absolute inset-0 bg-black/62" />

        <div className="relative mx-auto max-w-[1720px] px-4 sm:px-6 lg:px-8">
          <header className="flex justify-end py-5">
            {authEnabled ? (
              <Button
                variant="secondary"
                className="h-10 rounded-full border-[#27272a] bg-black/50 px-4 text-white hover:bg-[#121212]"
                onClick={() => void handleLogout()}
                disabled={loggingOut}
              >
                {loggingOut ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : <LogOut className="mr-2 h-4 w-4" />}
                退出登录
              </Button>
            ) : null}
          </header>

          <div className="grid gap-6 pb-10 pt-10 lg:grid-cols-[minmax(0,1.2fr)_330px] lg:items-end lg:pb-14 lg:pt-20">
            <div className="max-w-4xl space-y-6">
              <h1 className="runway-title text-[44px] font-semibold text-white sm:text-[60px]">抖音视频库</h1>

              {featuredVideo ? (
                <div className="runway-panel rounded-[24px] p-5 sm:p-6">
                  <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
                    <div className="space-y-3">
                      <div className="runway-label">当前焦点</div>
                      <div className="max-w-3xl text-[28px] font-semibold tracking-[-0.05em] text-white">
                        {getDisplayTitle(featuredVideo)}
                      </div>
                      <div className="flex flex-wrap gap-4 text-sm text-[#a7a7a7]">
                        <span>{featuredVideo.author || "未知作者"}</span>
                        <span>{formatDuration(featuredVideo.duration)}</span>
                        <span>{formatDate(featuredVideo.savedAt)}</span>
                      </div>
                    </div>

                    <Button
                      variant="secondary"
                      className="h-11 rounded-full border-white bg-white px-5 text-black hover:bg-[#eceff4]"
                      onClick={() => setSelectedVideo(featuredVideo)}
                    >
                      <Play className="mr-2 h-4 w-4 fill-current" />
                      打开视频
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="runway-panel rounded-[24px] p-5 text-sm text-[#a7a7a7]">
                  还没有已保存视频。先在下方粘贴分享文案，保存第一条素材后这里会自动展示当前焦点。
                </div>
              )}
            </div>

            <aside className="runway-panel rounded-[24px] p-5 sm:p-6">
              <div className="runway-label">Library state</div>
              <div className="mt-6 grid gap-5">
                <StatCard label="当前结果" value={String(stats.total)} />
                <StatCard label="已评分" value={String(stats.rated)} />
                <StatCard label="总时长" value={`${stats.totalDurationMinutes} 分钟`} />
              </div>
            </aside>
          </div>
        </div>
      </div>

      <div className="mx-auto flex w-full max-w-[1720px] flex-col gap-6 px-4 py-8 sm:px-6 lg:px-8">
        <section className="grid gap-6 xl:grid-cols-[minmax(0,1.2fr)_360px]">
          <Card className="runway-panel rounded-[24px] border-[#27272a] bg-[#030303] shadow-none">
            <CardHeader className="gap-3">
              <div className="runway-label">Ingest</div>
              <div className="space-y-2">
                <CardTitle className="text-[28px] font-semibold tracking-[-0.04em] text-white">获取视频</CardTitle>
                <CardDescription className="max-w-2xl text-[15px] leading-7 text-[#a7a7a7]">
                  粘贴整段分享文案即可，后端会自动提取抖音短链并下载本地文件。
                </CardDescription>
              </div>
            </CardHeader>

            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="share-text" className="text-sm text-white">
                  分享内容
                </Label>
                <Textarea
                  id="share-text"
                  value={shareText}
                  onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setShareText(event.target.value)}
                  placeholder="粘贴抖音分享链接或整段分享文案"
                  className="min-h-36 rounded-2xl border-[#27272a] bg-[#0f0f10] px-4 py-3 text-base text-white placeholder:text-[#6f7680] focus:border-white/35 focus:ring-white/10"
                />
              </div>

              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <Button
                  variant="secondary"
                  size="lg"
                  className="h-12 rounded-full border-white bg-white px-6 text-black hover:bg-[#eceff4]"
                  onClick={() => void handleDownload()}
                  disabled={submitting}
                >
                  {submitting ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
                  获取视频并保存到本地
                </Button>
                <div className="text-sm text-[#7d848e]">支持整段分享文案，后端会自动解析短链。</div>
              </div>

              {message ? (
                <div className="rounded-2xl border border-emerald-500/20 bg-emerald-950/40 px-4 py-3 text-sm leading-6 text-emerald-100">
                  {message}
                </div>
              ) : null}

              {error ? (
                <div className="rounded-2xl border border-rose-500/20 bg-rose-950/40 px-4 py-3 text-sm leading-6 text-rose-100">
                  {error}
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Card className="runway-panel rounded-[24px] border-[#27272a] bg-[#030303] shadow-none">
            <CardHeader className="gap-3">
              <div className="runway-label">Taxonomy</div>
              <div className="space-y-2">
                <CardTitle className="text-[24px] font-semibold tracking-[-0.04em] text-white">标签管理</CardTitle>
                <CardDescription className="text-[15px] leading-7 text-[#a7a7a7]">
                  创建常用标签，用于后续筛选和在视频详情里进行多选绑定。
                </CardDescription>
              </div>
            </CardHeader>

            <CardContent className="space-y-4">
              <div className="flex flex-col gap-2 sm:flex-row">
                <Input
                  value={newTagName}
                  onChange={(event: ChangeEvent<HTMLInputElement>) => setNewTagName(event.target.value)}
                  placeholder="例如：舞蹈、素材、复盘"
                  className="h-11 rounded-full border-[#27272a] bg-[#0f0f10] px-4 text-white placeholder:text-[#6f7680] focus:border-white/35 focus:ring-white/10"
                />
                <Button
                  variant="secondary"
                  className="h-11 shrink-0 rounded-full border-[#27272a] bg-[#101010] px-5 text-white hover:bg-[#171717]"
                  onClick={() => void handleCreateTag()}
                  disabled={creatingTag}
                >
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
                        "flex items-center overflow-hidden rounded-full border",
                        selected ? "border-white/30 bg-white/10" : "border-[#27272a] bg-[#0a0a0a]",
                      )}
                    >
                      <button
                        type="button"
                        onClick={() => toggleTagFilter(tag.id)}
                        className={cn(
                          "flex items-center gap-2 px-4 py-2 text-sm transition-colors",
                          selected ? "text-white" : "text-[#c8cdd5] hover:bg-[#111111]",
                        )}
                      >
                        <span>{tag.name}</span>
                        <span className="text-xs text-[#7d848e]">{tag.videoCount}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => void handleDeleteTag(tag)}
                        disabled={deletingTagId === tag.id}
                        className="border-l border-white/10 px-3 py-2 text-[#7d848e] transition-colors hover:bg-rose-950/40 hover:text-rose-200 disabled:opacity-50"
                      >
                        {deletingTagId === tag.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <X className="h-3.5 w-3.5" />}
                      </button>
                    </div>
                  );
                })}
              </div>

              {tags.length === 0 ? (
                <div className="rounded-2xl border border-[#27272a] bg-[#0a0a0a] px-4 py-3 text-sm leading-6 text-[#a7a7a7]">
                  还没有标签。先创建几个常用标签，后面可以在视频详情里多选绑定。
                </div>
              ) : (
                <div className="text-sm leading-6 text-[#7d848e]">
                  点击标签名称可加入筛选；点击右侧关闭按钮会删除该标签及其在所有视频上的绑定关系。
                </div>
              )}
            </CardContent>
          </Card>
        </section>

        <section className="space-y-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div className="space-y-2">
              <div className="runway-label">Archive</div>
              <h2 className="text-[32px] font-semibold tracking-[-0.05em] text-white">已保存的视频</h2>
            </div>

            <Button
              variant="secondary"
              className="h-10 rounded-full border-[#27272a] bg-[#080808] px-4 text-white hover:bg-[#121212]"
              onClick={() => void Promise.all([loadVideos(ratingFilter, selectedTagIds), loadTags()])}
              disabled={loading}
            >
              {loading ? <LoaderCircle className="mr-2 h-4 w-4 animate-spin" /> : null}
              刷新列表
            </Button>
          </div>

          <div className="runway-panel rounded-[24px] p-5">
            <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
              <div className="space-y-3">
                <div className="runway-label">评分筛选</div>
                <div className="flex flex-wrap gap-2">
                  {filterOptions.map((option) => {
                    const active = ratingFilter === option.value;
                    return (
                      <Button
                        key={option.value}
                        variant="secondary"
                        size="sm"
                        className={cn(
                          "h-9 rounded-full px-4",
                          active
                            ? "border-white bg-white text-black hover:bg-[#eceff4]"
                            : "border-[#27272a] bg-[#080808] text-[#d0d4db] hover:bg-[#111111]",
                        )}
                        onClick={() => setRatingFilter(option.value)}
                      >
                        {option.label}
                      </Button>
                    );
                  })}
                </div>
              </div>

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="runway-label">标签筛选</div>
                  <div className="text-xs text-[#7d848e]">多选时按交集过滤</div>
                </div>

                {tags.length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {tags.map((tag) => {
                      const selected = selectedTagIds.includes(tag.id);
                      return (
                        <Button
                          key={tag.id}
                          variant="secondary"
                          size="sm"
                          className={cn(
                            "h-9 rounded-full px-4",
                            selected
                              ? "border-white bg-white text-black hover:bg-[#eceff4]"
                              : "border-[#27272a] bg-[#080808] text-[#d0d4db] hover:bg-[#111111]",
                          )}
                          onClick={() => toggleTagFilter(tag.id)}
                        >
                          <Tag className="mr-2 h-3.5 w-3.5" />
                          {tag.name}
                        </Button>
                      );
                    })}
                  </div>
                ) : (
                  <div className="text-sm text-[#a7a7a7]">还没有标签，先在上方创建后再筛选。</div>
                )}
              </div>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {videos.map((video, index) => {
              const featuredCard = index === 0;
              return (
                <button
                  key={video.videoId}
                  type="button"
                  onClick={() => setSelectedVideo(video)}
                  className={cn("group text-left", featuredCard && "sm:col-span-2 xl:col-span-2")}
                >
                  <article className="overflow-hidden rounded-[24px] border border-[#27272a] bg-[#050505] transition-colors group-hover:border-white/20">
                    <div className={cn("relative", featuredCard ? "h-[420px]" : "aspect-[4/5]")}>
                      <img
                        src={video.coverUrl}
                        alt={getDisplayTitle(video)}
                        className="h-full w-full object-cover"
                        loading="lazy"
                      />
                      <div className="absolute inset-0 bg-black/28" />

                      <div className="absolute inset-0 flex flex-col justify-between p-4 sm:p-5">
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge className="border border-white/12 bg-black/36 text-white">{video.author || "未署名作者"}</Badge>
                          {video.rating > 0 ? (
                            <Badge className="border border-white/12 bg-white/12 text-white">评分 {video.rating}/5</Badge>
                          ) : (
                            <Badge className="border border-white/12 bg-black/30 text-[#c5cad1]">未评分</Badge>
                          )}
                        </div>

                        <div className="space-y-3">
                          {video.tags.length > 0 ? (
                            <div className="flex flex-wrap gap-2">
                              {video.tags.slice(0, 3).map((tag) => (
                                <Badge key={tag.id} className="border border-white/10 bg-black/30 text-[#d0d4db]">
                                  {tag.name}
                                </Badge>
                              ))}
                            </div>
                          ) : null}

                          <h3
                            className={cn(
                              "max-w-[28rem] text-white",
                              featuredCard
                                ? "text-[28px] font-semibold tracking-[-0.05em] leading-[1.05]"
                                : "text-base font-medium leading-6",
                            )}
                          >
                            {getDisplayTitle(video)}
                          </h3>

                          <div className="flex items-center justify-between gap-3 text-xs text-[#b0b3b8]">
                            <span>{formatDuration(video.duration)}</span>
                            <span>{formatDate(video.savedAt)}</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  </article>
                </button>
              );
            })}

            {!loading && videos.length === 0 ? (
              <Card className="col-span-full rounded-[24px] border-[#27272a] bg-[#050505] shadow-none">
                <CardContent className="flex min-h-72 flex-col items-center justify-center py-10 text-center">
                  <Play className="h-10 w-10 text-[#6f7680]" />
                  <h3 className="mt-4 text-base font-semibold text-white">当前筛选下没有视频</h3>
                  <p className="mt-2 max-w-md text-sm leading-6 text-[#a7a7a7]">
                    你可以先在上方保存一个抖音视频，或者调整评分与标签筛选条件查看其他内容。
                  </p>
                </CardContent>
              </Card>
            ) : null}
          </div>
        </section>
      </div>

      <Dialog open={Boolean(selectedVideo)} onOpenChange={(open: boolean) => !open && setSelectedVideo(null)}>
        {selectedVideo ? (
          <DialogContent className="max-h-[92vh] w-[min(1320px,calc(100%-24px))] overflow-y-auto border-[#27272a] bg-[#030303] p-0 text-white shadow-none">
            <DialogHeader className="sr-only">
              <DialogTitle>{getDisplayTitle(selectedVideo)}</DialogTitle>
            </DialogHeader>

            <div className="grid gap-0 lg:grid-cols-[minmax(0,1.45fr)_360px]">
              <div className="space-y-5 p-5 sm:p-6">
                <div className="runway-surface flex flex-col gap-3 rounded-[22px] px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <div className="runway-label">当前筛选结果</div>
                    <div className="mt-2 text-sm text-[#a7a7a7]">
                      {selectedVideoIndex >= 0
                        ? `第 ${selectedVideoIndex + 1} 个，共 ${videos.length} 个`
                        : videos.length > 0
                          ? "当前视频不在当前筛选结果中，无法切换。"
                          : "当前筛选结果为空。"}
                    </div>
                  </div>

                  <div className="flex gap-2">
                    <Button
                      variant="secondary"
                      size="sm"
                      className="h-9 rounded-full border-[#27272a] bg-[#080808] px-4 text-white hover:bg-[#121212]"
                      onClick={() => selectAdjacentVideo(-1)}
                      disabled={!canSelectPreviousVideo}
                    >
                      <ChevronLeft className="mr-1 h-3.5 w-3.5" />
                      上一个
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      className="h-9 rounded-full border-[#27272a] bg-[#080808] px-4 text-white hover:bg-[#121212]"
                      onClick={() => selectAdjacentVideo(1)}
                      disabled={!canSelectNextVideo}
                    >
                      下一个
                      <ChevronRight className="ml-1 h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>

                <div className="overflow-hidden rounded-[24px] border border-[#27272a] bg-black p-2">
                  <video
                    key={selectedVideo.videoId}
                    src={selectedVideo.localUrl}
                    poster={selectedVideo.coverUrl}
                    autoPlay
                    controls
                    loop
                    playsInline
                    className="mx-auto block max-h-[70vh] w-auto max-w-full bg-black"
                  />
                </div>

                <div className="space-y-4">
                  <div className="runway-label">Title</div>
                  <h2 className="text-[30px] font-semibold tracking-[-0.06em] text-white">
                    {getDisplayTitle(selectedVideo)}
                  </h2>

                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    <Metric label="时长" value={formatDuration(selectedVideo.duration)} />
                    <Metric label="尺寸" value={formatVideoResolution(selectedVideo.videoWidth, selectedVideo.videoHeight)} />
                    <Metric label="点赞" value={formatCompactNumber(selectedVideo.likeCount)} />
                    <Metric label="评论" value={formatCompactNumber(selectedVideo.commentCount)} />
                  </div>
                </div>
              </div>

              <div className="border-t border-[#27272a] p-5 sm:p-6 lg:border-l lg:border-t-0">
                <div className="space-y-4">
                  <Card className="runway-surface rounded-[22px] border-[#27272a] bg-[#0a0a0a] shadow-none">
                    <CardHeader className="pb-4">
                      <CardTitle className="text-sm text-white">评分</CardTitle>
                      <CardDescription className="text-sm text-[#7d848e]">评分会和分享链接一起保存在 SQLite 中。</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-3">
                      <div className="grid grid-cols-5 gap-2">
                        {[1, 2, 3, 4, 5].map((score) => {
                          const active = selectedVideo.rating === score;
                          return (
                            <Button
                              key={score}
                              variant="secondary"
                              className={cn(
                                "h-10 gap-1 rounded-full",
                                active
                                  ? "border-white bg-white text-black hover:bg-[#eceff4]"
                                  : "border-[#27272a] bg-[#080808] text-white hover:bg-[#111111]",
                                ratingVideoId === selectedVideo.videoId && "opacity-70",
                              )}
                              onClick={() => void handleRate(selectedVideo.videoId, score)}
                              disabled={ratingVideoId === selectedVideo.videoId}
                            >
                              <Star className="h-3.5 w-3.5" />
                              {score}
                            </Button>
                          );
                        })}
                      </div>
                      <p className="text-xs leading-5 text-[#7d848e]">
                        当前评分：{selectedVideo.rating > 0 ? `${selectedVideo.rating}/5` : "未评分"}
                      </p>
                    </CardContent>
                  </Card>

                  <Card className="runway-surface rounded-[22px] border-[#27272a] bg-[#0a0a0a] shadow-none">
                    <CardHeader className="pb-4">
                      <CardTitle className="text-sm text-white">标签</CardTitle>
                      <CardDescription className="text-sm text-[#7d848e]">可为当前视频绑定多个标签。</CardDescription>
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
                                variant="secondary"
                                className={cn(
                                  "h-9 rounded-full px-4",
                                  selected
                                    ? "border-white bg-white text-black hover:bg-[#eceff4]"
                                    : "border-[#27272a] bg-[#080808] text-white hover:bg-[#111111]",
                                )}
                                onClick={() => void handleToggleVideoTag(selectedVideo, tag.id)}
                                disabled={taggingVideoId === selectedVideo.videoId}
                              >
                                <Tag className="mr-2 h-3.5 w-3.5" />
                                {tag.name}
                              </Button>
                            );
                          })}
                        </div>
                      ) : (
                        <div className="text-sm text-[#a7a7a7]">还没有标签，先在上方创建后再给视频绑定。</div>
                      )}

                      <div className="rounded-2xl border border-[#27272a] bg-[#080808] px-4 py-3 text-sm leading-6 text-[#a7a7a7]">
                        当前标签：
                        {selectedVideo.tags.length > 0
                          ? ` ${selectedVideo.tags.map((tag) => tag.name).join("、")}`
                          : " 未绑定标签"}
                      </div>
                    </CardContent>
                  </Card>

                  <Card className="runway-surface rounded-[22px] border-[#27272a] bg-[#0a0a0a] shadow-none">
                    <CardHeader className="pb-4">
                      <CardTitle className="text-sm text-white">来源信息</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-3 text-sm text-[#a7a7a7]">
                      <InfoRow label="作者昵称" value={selectedVideo.author} />
                      <InfoRow label="抖音号" value={selectedVideo.authorId || "未获取"} />
                      <InfoRow label="封面来源" value={selectedVideo.coverSourceUrl || selectedVideo.coverUrl} />
                      <InfoRow label="本地封面" value={selectedVideo.coverLocalFile || "未本地化"} />
                      <InfoRow label="分享短链" value={selectedVideo.shareUrl} />
                      <InfoRow label="下载地址" value={selectedVideo.downloadUrl} />
                      <InfoRow label="原始分享页" value={selectedVideo.originalUrl} />
                      <InfoRow label="本地大小" value={formatFileSize(selectedVideo.fileSize)} />
                    </CardContent>
                  </Card>

                  <Card className="runway-surface rounded-[22px] border-[#27272a] bg-[#0a0a0a] shadow-none">
                    <CardHeader className="pb-4">
                      <CardTitle className="text-sm text-white">原始文案</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <div className="rounded-2xl border border-[#27272a] bg-[#080808] px-4 py-3 text-sm leading-6 text-[#a7a7a7]">
                        {selectedVideo.lastSourceInput}
                      </div>
                    </CardContent>
                  </Card>

                  <Card className="runway-surface rounded-[22px] border-[#4b1f24] bg-[#120608] shadow-none">
                    <CardHeader className="pb-4">
                      <CardTitle className="text-sm text-white">删除视频</CardTitle>
                      <CardDescription className="text-sm text-rose-200/75">
                        会同时删除本地 MP4 文件、封面文件和 SQLite 里的元数据记录。
                      </CardDescription>
                    </CardHeader>
                    <CardContent>
                      <Button
                        variant="secondary"
                        className="h-11 w-full rounded-full border-rose-500/30 bg-rose-950/40 text-rose-100 hover:bg-rose-950/60"
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
                </div>
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
    <div className="border-b border-white/10 pb-4">
      <div className="runway-label">{label}</div>
      <div className="mt-3 text-[32px] font-semibold tracking-[-0.06em] text-white">{value}</div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="runway-surface rounded-[20px] px-4 py-4">
      <div className="runway-label">{label}</div>
      <div className="mt-3 text-base font-semibold text-white">{value}</div>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-2">
      <div className="runway-label">{label}</div>
      <div className="break-all rounded-2xl border border-[#27272a] bg-[#080808] px-3 py-3 font-mono text-[12px] leading-5 text-[#eef2f7]">
        {value}
      </div>
    </div>
  );
}

function getDisplayTitle(video: VideoRecord) {
  return video.title || video.description || "未命名视频";
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

function formatCompactNumber(value: number) {
  if (value >= 10000) {
    return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)} 万`;
  }

  return String(value);
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
