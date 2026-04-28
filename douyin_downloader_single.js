#!/usr/bin/env node

/**
 * 抖音视频直链获取工具 - 单文件版本
 * 支持解析抖音分享链接并返回视频/图片集下载直链
 * 
 * 使用方法:
 * node douyin_downloader_single.js "https://v.douyin.com/ww8JpyDgr1o/"
 */

const axios = require('axios');
const cheerio = require('cheerio');

// 配置
const CONFIG = {
  // 请求配置
  requestTimeout: 10000,
  maxRetries: 3,
  retryDelay: 2000,
  
  // 随机延时范围（毫秒）
  randomDelayMin: 1000,
  randomDelayMax: 3000,
  
  // 抖音相关配置
  douyinDomain: 'v.douyin.com',
  douyinApiBase: 'https://www.iesdouyin.com'
};

/**
 * 简单的日志记录器
 */
const isQuiet = process.env.QUIET === '1';

const logger = {
  info: (msg) => !isQuiet && console.log(`[INFO] ${msg}`),
  warn: (msg) => !isQuiet && console.warn(`[WARN] ${msg}`),
  error: (msg) => !isQuiet && console.error(`[ERROR] ${msg}`),
  debug: (msg) => !isQuiet && process.env.DEBUG && console.log(`[DEBUG] ${msg}`)
};

/**
 * 随机延时
 * @param {number} minSeconds 最小延时秒数
 * @param {number} maxSeconds 最大延时秒数
 * @returns {Promise} Promise对象
 */
function randomDelay(minSeconds = 1, maxSeconds = 3) {
  const delay = Math.random() * (maxSeconds - minSeconds) + minSeconds;
  return new Promise(resolve => setTimeout(resolve, delay * 1000));
}

/**
 * 从文本中提取抖音分享链接
 * @param {string} text 包含抖音链接的文本
 * @returns {string[]} 抖音链接列表
 */
function extractDouyinLinks(text) {
  // 支持多种抖音链接格式
  const patterns = [
    /https:\/\/v\.douyin\.com\/[A-Za-z0-9_-]+\//g,
    /https:\/\/www\.douyin\.com\/video\/\d+/g,
    /https:\/\/www\.douyin\.com\/note\/\d+/g
  ];
  
  let allMatches = [];
  for (const pattern of patterns) {
    const matches = text.match(pattern) || [];
    allMatches = allMatches.concat(matches);
  }
  
  return [...new Set(allMatches)]; // 去重
}

/**
 * 验证是否为有效的抖音链接
 * @param {string} url 待验证的URL
 * @returns {boolean} 是否为有效链接
 */
function isValidDouyinLink(url) {
  try {
    const urlObj = new URL(url);
    return (
      urlObj.hostname === 'v.douyin.com' || 
      urlObj.hostname === 'www.douyin.com'
    ) && urlObj.pathname.length > 1;
  } catch (e) {
    return false;
  }
}

/**
 * 从URL中提取内容ID（视频或图片集）
 * @param {string} url 页面URL
 * @returns {string|null} 内容ID，如果提取失败返回null
 */
function extractContentIdFromUrl(url) {
  try {
    // 从URL中提取内容ID的正则表达式
    const patterns = [
      /\/video\/(\d+)/,
      /\/note\/(\d+)/,
      /aweme_id=(\d+)/,
      /item_ids=(\d+)/
    ];
    
    for (const pattern of patterns) {
      const match = url.match(pattern);
      if (match) {
        return match[1];
      }
    }
    
    // 尝试从查询参数中获取
    const urlObj = new URL(url);
    const contentId = urlObj.searchParams.get('aweme_id') ||
                     urlObj.searchParams.get('item_ids');
    
    return contentId;
  } catch (e) {
    return null;
  }
}

/**
 * 识别抖音链接的内容类型
 * @param {string} url 抖音页面URL
 * @returns {string} 内容类型：'video'（视频）或 'note'（图片集）
 */
function identifyContentType(url) {
  if (url.includes('/share/video/') || url.includes('/video/')) {
    return 'video';
  } else if (url.includes('/share/note/') || url.includes('/note/')) {
    return 'note';
  } else {
    // 无法确定，默认返回video
    return 'video';
  }
}

/**
 * 清理文本，去除多余空白和特殊字符
 * @param {string} text 待清理的文本
 * @returns {string} 清理后的文本
 */
function cleanText(text) {
  if (!text) return '';
  
  // 去除HTML标签
  text = text.replace(/<[^>]+>/g, '');
  
  // 去除多余空白
  text = text.replace(/\s+/g, ' ').trim();
  
  return text;
}

/**
 * 将抖音播放地址归一化为更适合服务端下载的链接
 * @param {string|null} url 原始播放地址
 * @returns {string|null} 归一化后的地址
 */
function normalizeVideoUrl(url) {
  if (!url) return null;

  try {
    const urlObj = new URL(url);

    if (urlObj.pathname.includes('/playwm/')) {
      urlObj.pathname = urlObj.pathname.replace('/playwm/', '/play/');
      urlObj.searchParams.delete('logo_name');
    }

    return urlObj.toString();
  } catch (e) {
    return url.replace('/playwm/', '/play/').replace(/([?&])logo_name=[^&]*&?/, '$1').replace(/[?&]$/, '');
  }
}

/**
 * HTTP请求处理类
 */
class HttpClient {
  constructor() {
    this.setupAxios();
  }

  /**
   * 设置axios实例
   */
  setupAxios() {
    // 设置请求头，模拟真实浏览器
    const headers = {
      'User-Agent': 'Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1',
      'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8',
      'Accept-Language': 'zh-CN,zh;q=0.9,en;q=0.8',
      'Accept-Encoding': 'gzip, deflate',
      'Connection': 'keep-alive',
      'Upgrade-Insecure-Requests': '1',
      'Cache-Control': 'max-age=0'
    };

    this.axios = axios.create({
      headers,
      timeout: CONFIG.requestTimeout,
      maxRedirects: 5,
      validateStatus: function (status) {
        return status >= 200 && status < 300;
      }
    });
  }

  /**
   * 发送GET请求
   * @param {string} url 请求URL
   * @param {Object} options 请求选项
   * @returns {Promise<Object>} 响应对象，失败时返回null
   */
  async get(url, options = {}) {
    try {
      // 随机延时，避免请求过于频繁
      await randomDelay(CONFIG.randomDelayMin / 1000, CONFIG.randomDelayMax / 1000);
      
      logger.info(`发送GET请求: ${url}`);
      
      const response = await this.axios.get(url, {
        maxRedirects: options.maxRedirects !== false ? 5 : 0,
        ...options
      });
      
      logger.info(`请求成功，状态码: ${response.status}`);
      return response;
      
    } catch (error) {
      if (error.code === 'ECONNABORTED') {
        logger.error(`请求超时: ${url}`);
      } else if (error.code === 'ECONNRESET' || error.code === 'ENOTFOUND') {
        logger.error(`连接错误: ${url}`);
      } else if (error.response) {
        logger.error(`HTTP错误: ${error.response.status}, URL: ${url}`);
      } else {
        logger.error(`请求异常: ${error.message}, URL: ${url}`);
      }
      return null;
    }
  }

  /**
   * 获取短链接重定向后的最终URL
   * @param {string} shortUrl 短链接
   * @returns {Promise<string|null>} 最终URL，失败时返回null
   */
  async getFinalUrl(shortUrl) {
    try {
      // 使用HEAD请求获取重定向信息，不下载内容
      const response = await this.axios.head(shortUrl, {
        maxRedirects: 5,
        timeout: CONFIG.requestTimeout
      });
      
      if (response.status === 200) {
        const finalUrl = response.request.res.responseUrl || response.request._redirectable._options.href;
        logger.info(`短链接 ${shortUrl} 重定向到: ${finalUrl}`);
        return finalUrl;
      } else {
        logger.error(`获取最终URL失败，状态码: ${response.status}`);
        return null;
      }
      
    } catch (error) {
      logger.error(`获取最终URL异常: ${error.message}, URL: ${shortUrl}`);
      return null;
    }
  }
}

/**
 * 视频提取类
 */
class VideoExtractor {
  constructor() {
    this.httpClient = new HttpClient();
  }

  /**
   * 从抖音分享链接提取内容信息（视频或图片集）
   * @param {string} shareUrl 抖音分享链接
   * @returns {Promise<Object|null>} 内容信息字典，失败时返回null
   */
  async extractVideoInfo(shareUrl) {
    try {
      // 1. 获取重定向后的完整URL
      const finalUrl = await this.httpClient.getFinalUrl(shareUrl);
      if (!finalUrl) {
        logger.error(`无法获取重定向URL: ${shareUrl}`);
        return null;
      }
      
      // 2. 识别内容类型
      const contentType = identifyContentType(finalUrl);
      logger.info(`识别到内容类型: ${contentType}`);
      
      // 3. 提取内容ID
      const contentId = extractContentIdFromUrl(finalUrl);
      if (!contentId) {
        logger.error(`无法从URL中提取内容ID: ${finalUrl}`);
        return null;
      }
      
      logger.info(`提取到内容ID: ${contentId}`);
      logger.debug(`完整URL: ${finalUrl}`);
      
      // 4. 获取页面内容
      const response = await this.httpClient.get(finalUrl);
      if (!response) {
        logger.error(`无法获取页面内容: ${finalUrl}`);
        return null;
      }
      
      // 5. 解析页面内容
      logger.debug(`页面内容长度: ${response.data.length}`);
      let contentInfo;
      
      if (contentType === 'video') {
        contentInfo = await this.parsePageContent(response.data, contentId);
      } else if (contentType === 'note') {
        contentInfo = await this.parseNotePageContent(response.data, contentId);
      } else {
        logger.error(`不支持的内容类型: ${contentType}`);
        return null;
      }
      
      if (contentInfo) {
        contentInfo.share_url = shareUrl;
        contentInfo.original_url = finalUrl;
        contentInfo.content_type = contentType;
        contentInfo.video_id = contentId; // 保持向后兼容
        
        if (contentType === 'video') {
          logger.info(`成功提取视频信息: ${contentInfo.title || 'Unknown'}`);
        } else if (contentType === 'note') {
          logger.info(`成功提取图片集信息: ${contentInfo.title || 'Unknown'}`);
        }
      } else {
        logger.warn(`未能从页面内容中提取到完整信息，返回基本信息`);
        // 至少返回基本信息
        return {
          video_id: contentId, // 保持向后兼容
          content_type: contentType,
          title: 'Unknown Title',
          description: '',
          author: 'Unknown Author',
          video_url: null, // 视频为null
          image_urls: [], // 图片集为空数组
          cover_url: null,
          duration: 0,
          create_time: null,
          share_url: shareUrl,
          original_url: finalUrl
        };
      }
      
      return contentInfo;
      
    } catch (error) {
      logger.error(`提取内容信息失败: ${error.message}, URL: ${shareUrl}`);
      return null;
    }
  }

  /**
   * 解析图片集页面内容，提取图片集信息
   * @param {string} htmlContent 页面HTML内容
   * @param {string} noteId 图片集ID
   * @returns {Promise<Object|null>} 图片集信息字典
   */
  async parseNotePageContent(htmlContent, noteId) {
    try {
      // 方法1: 尝试从页面脚本中提取JSON数据
      logger.debug('尝试从页面脚本中提取图片集JSON数据');
      const jsonData = this.extractJsonFromScript(htmlContent);
      if (jsonData) {
        logger.debug('成功从页面脚本中提取JSON数据');
        return this.parseNoteJsonData(jsonData, noteId);
      }
      
      // 方法2: 尝试从页面HTML中提取基本信息
      logger.debug('尝试从HTML内容中提取图片集基本信息');
      const htmlInfo = this.parseNoteHtmlContent(htmlContent);
      if (htmlInfo) {
        return htmlInfo;
      }
      
      logger.warn('所有图片集提取方法都失败了');
      return null;
      
    } catch (error) {
      logger.error(`解析图片集页面内容失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 解析页面内容，提取视频信息
   * @param {string} htmlContent 页面HTML内容
   * @param {string} videoId 视频ID
   * @returns {Promise<Object|null>} 视频信息字典
   */
  async parsePageContent(htmlContent, videoId) {
    try {
      // 方法1: 尝试从页面脚本中提取JSON数据
      logger.debug('尝试从页面脚本中提取JSON数据');
      const jsonData = this.extractJsonFromScript(htmlContent);
      if (jsonData) {
        logger.debug('成功从页面脚本中提取JSON数据');
        return this.parseJsonData(jsonData, videoId);
      }
      
      // 方法2: 尝试通过API获取视频数据
      logger.debug('尝试通过API获取视频数据');
      const apiData = await this.getVideoApiData(videoId);
      if (apiData) {
        logger.debug('成功通过API获取视频数据');
        return this.parseApiData(apiData);
      }
      
      // 方法3: 尝试从页面HTML中提取基本信息
      logger.debug('尝试从HTML内容中提取基本信息');
      const htmlInfo = this.parseHtmlContent(htmlContent);
      if (htmlInfo) {
        return htmlInfo;
      }
      
      logger.warn('所有提取方法都失败了');
      return null;
      
    } catch (error) {
      logger.error(`解析页面内容失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从页面脚本中提取JSON数据
   * @param {string} htmlContent 页面HTML内容
   * @returns {Object|null} 提取的JSON数据，失败时返回null
   */
  extractJsonFromScript(htmlContent) {
    try {
      // 查找包含视频数据的脚本
      const patterns = [
        /window\._ROUTER_DATA\s*=\s*({.+?})<\/script>/,
        /window\._ROUTER_DATA\s*=\s*({.+?});/,
        /window\.__NUXT__\s*=\s*({.+?});/,
        /__DEFAULT_SCOPE__\s*=\s*({.+?});/,
        /videoDetail\s*=\s*({.+?});/,
        /window\.__INITIAL_STATE__\s*=\s*({.+?});/,
        /window\.__SSR_DATA__\s*=\s*({.+?});/,
        /<script id="RENDER_DATA" type="application\/json">([^<]+)<\/script>/,
        /<script[^>]*>.*?window\.SSR_HYDRATED_DATA\s*=\s*({.+?});.*?<\/script>/
      ];
      
      for (let i = 0; i < patterns.length; i++) {
        const pattern = patterns[i];
        logger.debug(`尝试模式 ${i + 1}: ${pattern}`);
        const match = htmlContent.match(pattern);
        
        if (match) {
          try {
            let jsonStr = match[1];
            // 清理可能的多余字符
            jsonStr = jsonStr.trim();
            if (jsonStr.startsWith(';')) {
              jsonStr = jsonStr.substring(1);
            }
            if (jsonStr.endsWith(';')) {
              jsonStr = jsonStr.slice(0, -1);
            }
            
            const data = JSON.parse(jsonStr);
            logger.info(`使用模式 ${i + 1} 成功提取JSON数据`);
            return data;
          } catch (parseError) {
            logger.debug(`模式 ${i + 1} 提取的JSON数据解析失败: ${parseError.message}`);
            continue;
          }
        }
      }
      
      logger.debug('所有JSON提取模式都失败了');
      return null;
      
    } catch (error) {
      logger.error(`从脚本中提取JSON数据失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 解析JSON数据，提取视频信息
   * @param {Object} jsonData JSON数据
   * @param {string} videoId 视频ID
   * @returns {Object|null} 视频信息字典
   */
  parseJsonData(jsonData, videoId) {
    try {
      // 尝试不同的数据结构路径
      let videoInfo = {};
      
      logger.debug('尝试从JSON数据中提取视频信息');
      logger.debug(`JSON数据键: ${Object.keys(jsonData)}`);
      
      // 路径1: 从loaderData中获取
      if (jsonData.loaderData) {
        logger.debug('从loaderData中提取');
        const loaderData = jsonData.loaderData;
        // 尝试不同的键
        for (const key in loaderData) {
          if (key.toLowerCase().includes('aweme') || key.toLowerCase().includes('video')) {
            const data = loaderData[key];
            if (data && data.awemeDetail) {
              const detail = data.awemeDetail;
              videoInfo = this.extractFromAwemeDetail(detail);
              if (videoInfo) break;
            } else if (data && data.detail) {
              const detail = data.detail;
              videoInfo = this.extractFromAwemeDetail(detail);
              if (videoInfo) break;
            } else if (data && data.videoInfoRes && data.videoInfoRes.item_list) {
              // 新的数据结构
              const items = data.videoInfoRes.item_list;
              if (items.length > 0) {
                const detail = items[0];
                videoInfo = this.extractFromAwemeDetail(detail);
                if (videoInfo) break;
              }
            }
          }
        }
      }
      
      // 路径2: 从state中获取
      if (!videoInfo.title && jsonData.state) {
        logger.debug('从state中提取');
        const stateData = jsonData.state;
        if (stateData.videoDetail) {
          const detail = stateData.videoDetail;
          videoInfo = this.extractFromAwemeDetail(detail);
        }
      }
      
      // 路径3: 直接查找awemeDetail
      if (!videoInfo.title && jsonData.awemeDetail) {
        logger.debug('直接从awemeDetail中提取');
        const detail = jsonData.awemeDetail;
        videoInfo = this.extractFromAwemeDetail(detail);
      }
      
      // 路径4: 查找其他可能的数据结构
      if (!videoInfo.title) {
        logger.debug('尝试其他数据结构');
        for (const key in jsonData) {
          const value = jsonData[key];
          if (value && typeof value === 'object') {
            if (value.awemeDetail) {
              const detail = value.awemeDetail;
              videoInfo = this.extractFromAwemeDetail(detail);
              if (videoInfo) break;
            } else if (value.videoInfoRes && value.videoInfoRes.item_list) {
              // 新的数据结构
              const items = value.videoInfoRes.item_list;
              if (items.length > 0) {
                const detail = items[0];
                videoInfo = this.extractFromAwemeDetail(detail);
                if (videoInfo) break;
              }
            }
          }
        }
      }
      
      logger.debug(`视频信息提取结果: ${!!videoInfo.title}`);
      return videoInfo;
      
    } catch (error) {
      logger.error(`解析JSON数据失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从awemeDetail中提取视频信息
   * @param {Object} detail awemeDetail数据
   * @returns {Object} 视频信息字典
   */
  extractFromAwemeDetail(detail) {
    try {
      const videoInfo = {};
      
      // 基本信息
      videoInfo.video_id = detail.aweme_id || '';
      videoInfo.title = cleanText(detail.desc || '');
      videoInfo.description = videoInfo.title;
      
      // 作者信息
      const authorInfo = detail.author || {};
      videoInfo.author = authorInfo.nickname || 'Unknown Author';
      videoInfo.author_id = authorInfo.unique_id || '';
      
      // 视频信息
      const videoData = detail.video || {};
      const playAddr = videoData.play_addr || {};
      const watermarkedUrls = playAddr.url_list || [];
      let videoUrls = watermarkedUrls;
      
      // 尝试获取无水印的视频链接
      if (!videoUrls || videoUrls.length === 0) {
        // 尝试其他可能的播放地址字段
        const addrKeys = ['download_addr', 'play_addr_h264', 'play_addr_lowbr'];
        for (const addrKey of addrKeys) {
          if (videoData[addrKey] && videoData[addrKey].url_list) {
            videoUrls = videoData[addrKey].url_list;
            break;
          }
        }
      }
      
      const rawVideoUrl = videoUrls.length > 0 ? videoUrls[0] : null;
      const normalizedVideoUrl = normalizeVideoUrl(rawVideoUrl);

      videoInfo.video_uri = playAddr.uri || '';
      videoInfo.watermark_video_url = watermarkedUrls.length > 0 ? watermarkedUrls[0] : rawVideoUrl;
      videoInfo.video_url = normalizedVideoUrl || rawVideoUrl;
      videoInfo.video_download_url = videoInfo.video_url;
      
      // 封面
      const coverInfo = videoData.cover || {};
      videoInfo.cover_url = coverInfo.url_list && coverInfo.url_list.length > 0 ? coverInfo.url_list[0] : '';
      
      // 时长
      const durationMs = videoData.duration || 0;
      videoInfo.duration = durationMs > 1000 ? durationMs / 1000 : durationMs; // 处理不同单位
      
      // 创建时间
      videoInfo.create_time = detail.create_time || 0;
      
      // 统计信息
      const statistics = detail.statistics || {};
      videoInfo.like_count = statistics.digg_count || 0;
      videoInfo.comment_count = statistics.comment_count || 0;
      videoInfo.share_count = statistics.share_count || 0;
      videoInfo.play_count = statistics.play_count || 0;
      videoInfo.collect_count = statistics.collect_count || 0;
      
      logger.debug(`提取到视频信息: title=${videoInfo.title}, author=${videoInfo.author}, video_url=${!!videoInfo.video_url}`);
      return videoInfo;
      
    } catch (error) {
      logger.error(`从awemeDetail中提取信息失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 通过API获取视频数据
   * @param {string} videoId 视频ID
   * @returns {Promise<Object|null>} API返回的数据，失败时返回null
   */
  async getVideoApiData(videoId) {
    try {
      // 尝试多个API端点
      const apiUrls = [
        `${CONFIG.douyinApiBase}/web/api/v2/aweme/iteminfo/?item_ids=${videoId}`,
        `https://www.iesdouyin.com/web/api/v2/aweme/iteminfo/?item_ids=${videoId}`,
        `https://www.douyin.com/aweme/v1/web/aweme/detail/?aweme_id=${videoId}`,
        `https://www.douyin.com/aweme/v1/web/aweme/detail/?aid=1128&aweme_id=${videoId}`
      ];
      
      for (const apiUrl of apiUrls) {
        logger.debug(`尝试API: ${apiUrl}`);
        const response = await this.httpClient.get(apiUrl);
        if (response) {
          try {
            const data = response.data;
            if (data.item_list && data.item_list.length > 0) {
              logger.info(`通过API ${apiUrl} 成功获取视频数据`);
              return data;
            }
          } catch (parseError) {
            logger.debug(`API ${apiUrl} 返回的数据不是有效的JSON`);
            continue;
          }
        }
      }
      
      logger.debug('所有API端点都失败了');
      return null;
      
    } catch (error) {
      logger.error(`通过API获取视频数据失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 解析API数据
   * @param {Object} apiData API返回的数据
   * @returns {Object|null} 视频信息字典
   */
  parseApiData(apiData) {
    try {
      logger.debug(`API数据键: ${Object.keys(apiData)}`);
      
      // 尝试不同的数据结构
      if (apiData.item_list && apiData.item_list.length > 0) {
        logger.debug('从item_list中解析');
        const item = apiData.item_list[0];
        return this.extractFromAwemeDetail(item);
      } else if (apiData.aweme_detail) {
        logger.debug('从aweme_detail中解析');
        const detail = apiData.aweme_detail;
        return this.extractFromAwemeDetail(detail);
      } else if (apiData.awemeDetail) {
        logger.debug('从awemeDetail中解析');
        const detail = apiData.awemeDetail;
        return this.extractFromAwemeDetail(detail);
      } else {
        logger.error('API数据中没有找到预期的视频信息结构');
        return null;
      }
      
    } catch (error) {
      logger.error(`解析API数据失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从HTML内容中解析基本信息
   * @param {string} htmlContent 页面HTML内容
   * @returns {Object} 视频信息字典
   */
  parseHtmlContent(htmlContent) {
    try {
      const $ = cheerio.load(htmlContent);
      const videoInfo = {};
      
      // 尝试从标题标签获取
      const titleTag = $('title').first();
      if (titleTag.length > 0) {
        let title = titleTag.text();
        // 清理标题，移除"抖音"等后缀
        title = title.replace(/[-_|]\s*抖音.*$/, '').trim();
        videoInfo.title = cleanText(title);
      }
      
      // 尝试从meta标签获取描述
      const descTag = $('meta[name="description"]').first();
      if (descTag.length > 0) {
        videoInfo.description = cleanText(descTag.attr('content') || '');
      }
      
      // 尝试获取视频URL
      const videoTag = $('video').first();
      if (videoTag.length > 0) {
        videoInfo.video_url = videoTag.attr('src') || videoTag.attr('data-src');
      }
      
      // 尝试获取封面图
      const coverTag = $('meta[property="og:image"]').first();
      if (coverTag.length > 0) {
        videoInfo.cover_url = coverTag.attr('content');
      }
      
      // 设置默认值
      videoInfo.title = videoInfo.title || 'Unknown Title';
      videoInfo.description = videoInfo.description || '';
      videoInfo.author = videoInfo.author || 'Unknown Author';
      videoInfo.video_url = videoInfo.video_url || null;
      videoInfo.cover_url = videoInfo.cover_url || null;
      videoInfo.duration = videoInfo.duration || 0;
      videoInfo.create_time = videoInfo.create_time || null;
      videoInfo.video_id = videoInfo.video_id || '';
      
      logger.debug(`HTML解析结果: title=${videoInfo.title}, author=${videoInfo.author}`);
      return videoInfo;
      
    } catch (error) {
      logger.error(`解析HTML内容失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 解析图片集JSON数据，提取图片集信息
   * @param {Object} jsonData JSON数据
   * @param {string} noteId 图片集ID
   * @returns {Object|null} 图片集信息字典
   */
  parseNoteJsonData(jsonData, noteId) {
    try {
      // 尝试不同的数据结构路径
      let noteInfo = {};
      
      logger.debug('尝试从JSON数据中提取图片集信息');
      logger.debug(`JSON数据键: ${Object.keys(jsonData)}`);
      
      // 路径1: 从loaderData中获取
      if (jsonData.loaderData) {
        logger.debug('从loaderData中提取图片集');
        const loaderData = jsonData.loaderData;
        logger.debug(`loaderData键: ${Object.keys(loaderData)}`);
        
        // 尝试不同的键
        for (const key in loaderData) {
          logger.debug(`检查loaderData键: ${key}`);
          if (key.toLowerCase().includes('note') || key.toLowerCase().includes('aweme')) {
            const data = loaderData[key];
            logger.debug(`找到相关键: ${key}, 数据类型: ${typeof data}`);
            
            if (data && data.awemeDetail) {
              logger.debug('从awemeDetail提取图片集');
              const detail = data.awemeDetail;
              noteInfo = this.extractFromNoteDetail(detail);
              if (noteInfo) break;
            } else if (data && data.detail) {
              logger.debug('从detail提取图片集');
              const detail = data.detail;
              noteInfo = this.extractFromNoteDetail(detail);
              if (noteInfo) break;
            } else if (data && data.noteInfoRes && data.noteInfoRes.item_list) {
              // 图片集数据结构
              logger.debug('从noteInfoRes提取图片集');
              const items = data.noteInfoRes.item_list;
              if (items.length > 0) {
                const detail = items[0];
                noteInfo = this.extractFromNoteDetail(detail);
                if (noteInfo) break;
              }
            } else if (data && data.images) {
              // 直接从images字段提取
              logger.debug('直接从images字段提取图片集');
              noteInfo = this.extractFromNoteDetail(data);
              if (noteInfo) break;
            } else if (key === 'note_layout' || key === 'note_(id)/page') {
              // 特殊处理note_layout和note_(id)/page
              logger.debug(`特殊处理键: ${key}`);
              noteInfo = this.extractFromNoteLayout(data);
              if (noteInfo) break;
            }
          }
        }
      }
      
      // 路径2: 从state中获取
      if (!noteInfo.title && jsonData.state) {
        logger.debug('从state中提取图片集');
        const stateData = jsonData.state;
        logger.debug(`state键: ${Object.keys(stateData)}`);
        
        if (stateData.noteDetail) {
          const detail = stateData.noteDetail;
          noteInfo = this.extractFromNoteDetail(detail);
        }
      }
      
      // 路径3: 直接查找awemeDetail
      if (!noteInfo.title && jsonData.awemeDetail) {
        logger.debug('直接从awemeDetail中提取图片集');
        const detail = jsonData.awemeDetail;
        noteInfo = this.extractFromNoteDetail(detail);
      }
      
      // 路径4: 查找其他可能的数据结构
      if (!noteInfo.title) {
        logger.debug('尝试其他图片集数据结构');
        for (const key in jsonData) {
          const value = jsonData[key];
          if (value && typeof value === 'object') {
            logger.debug(`检查键: ${key}`);
            if (value.awemeDetail) {
              const detail = value.awemeDetail;
              noteInfo = this.extractFromNoteDetail(detail);
              if (noteInfo) break;
            } else if (value.noteInfoRes && value.noteInfoRes.item_list) {
              // 图片集数据结构
              const items = value.noteInfoRes.item_list;
              if (items.length > 0) {
                const detail = items[0];
                noteInfo = this.extractFromNoteDetail(detail);
                if (noteInfo) break;
              }
            } else if (value.images) {
              // 直接从images字段提取
              noteInfo = this.extractFromNoteDetail(value);
              if (noteInfo) break;
            }
          }
        }
      }
      
      logger.debug(`图片集信息提取结果: ${!!noteInfo.title}`);
      return noteInfo;
      
    } catch (error) {
      logger.error(`解析图片集JSON数据失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从noteDetail中提取图片集信息
   * @param {Object} detail noteDetail数据
   * @returns {Object} 图片集信息字典
   */
  extractFromNoteDetail(detail) {
    try {
      const noteInfo = {};
      
      // 基本信息
      noteInfo.video_id = detail.aweme_id || '';
      noteInfo.title = cleanText(detail.desc || '');
      noteInfo.description = noteInfo.title;
      
      // 作者信息
      const authorInfo = detail.author || {};
      noteInfo.author = authorInfo.nickname || 'Unknown Author';
      noteInfo.author_id = authorInfo.unique_id || '';
      
      // 图片信息
      const imageData = detail.images || [];
      const imageUrls = [];
      
      if (imageData.length > 0) {
        for (const img of imageData) {
          if (img.url_list && img.url_list.length > 0) {
            imageUrls.push(img.url_list[0]);
          }
        }
      }
      
      // 如果没有找到图片，尝试其他可能的数据结构
      if (imageUrls.length === 0) {
        // 尝试从其他字段获取图片
        const possibleImageFields = ['image', 'cover', 'thumb'];
        for (const field of possibleImageFields) {
          if (detail[field] && detail[field].url_list) {
            for (const url of detail[field].url_list) {
              imageUrls.push(url);
            }
          }
        }
      }
      
      noteInfo.image_urls = imageUrls;
      
      // 封面（使用第一张图片作为封面）
      noteInfo.cover_url = imageUrls.length > 0 ? imageUrls[0] : '';
      
      // 时长（图片集没有时长，设为0）
      noteInfo.duration = 0;
      
      // 创建时间
      noteInfo.create_time = detail.create_time || 0;
      
      // 统计信息
      const statistics = detail.statistics || {};
      noteInfo.like_count = statistics.digg_count || 0;
      noteInfo.comment_count = statistics.comment_count || 0;
      noteInfo.share_count = statistics.share_count || 0;
      noteInfo.play_count = 0; // 图片集没有播放量
      noteInfo.collect_count = statistics.collect_count || 0;
      
      // 图片集特有的字段
      noteInfo.image_count = imageUrls.length;
      
      logger.debug(`提取到图片集信息: title=${noteInfo.title}, author=${noteInfo.author}, image_count=${noteInfo.image_count}`);
      return noteInfo;
      
    } catch (error) {
      logger.error(`从noteDetail中提取信息失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从note_layout数据中提取图片集信息
   * @param {Object} layoutData note_layout数据
   * @returns {Object} 图片集信息字典
   */
  extractFromNoteLayout(layoutData) {
    try {
      logger.debug(`从note_layout提取图片集，数据键: ${Object.keys(layoutData)}`);
      
      const noteInfo = {};
      
      // 尝试从videoInfoRes获取图片信息
      if (layoutData.videoInfoRes) {
        logger.debug('从videoInfoRes提取图片集信息');
        const videoInfoRes = layoutData.videoInfoRes;
        
        if (videoInfoRes.item_list && videoInfoRes.item_list.length > 0) {
          const item = videoInfoRes.item_list[0];
          return this.extractFromNoteDetail(item);
        }
      }
      
      // 尝试从不同字段获取信息
      if (layoutData.noteInfo) {
        const noteInfoData = layoutData.noteInfo;
        noteInfo.title = cleanText(noteInfoData.title || noteInfoData.desc || '');
        noteInfo.description = noteInfo.title;
        
        // 作者信息
        if (noteInfoData.author) {
          noteInfo.author = noteInfoData.author.nickname || noteInfoData.author.name || 'Unknown Author';
          noteInfo.author_id = noteInfoData.author.unique_id || noteInfoData.author.id || '';
        }
        
        // 图片信息
        if (noteInfoData.images && Array.isArray(noteInfoData.images)) {
          const imageUrls = [];
          for (const img of noteInfoData.images) {
            if (img.url_list && img.url_list.length > 0) {
              imageUrls.push(img.url_list[0]);
            } else if (img.url) {
              imageUrls.push(img.url);
            }
          }
          noteInfo.image_urls = imageUrls;
          noteInfo.cover_url = imageUrls.length > 0 ? imageUrls[0] : '';
          noteInfo.image_count = imageUrls.length;
        }
        
        // 统计信息
        if (noteInfoData.statistics) {
          const statistics = noteInfoData.statistics;
          noteInfo.like_count = statistics.digg_count || 0;
          noteInfo.comment_count = statistics.comment_count || 0;
          noteInfo.share_count = statistics.share_count || 0;
          noteInfo.collect_count = statistics.collect_count || 0;
        }
        
        // 创建时间
        noteInfo.create_time = noteInfoData.create_time || 0;
        
        logger.debug(`从note_layout提取到图片集信息: title=${noteInfo.title}, image_count=${noteInfo.image_count}`);
        return noteInfo;
      }
      
      // 尝试从其他字段获取
      if (layoutData.noteDetail) {
        return this.extractFromNoteDetail(layoutData.noteDetail);
      }
      
      // 尝试直接从layoutData获取
      if (layoutData.title || layoutData.desc) {
        noteInfo.title = cleanText(layoutData.title || layoutData.desc || '');
        noteInfo.description = noteInfo.title;
        
        if (layoutData.author) {
          noteInfo.author = layoutData.author.nickname || layoutData.author.name || 'Unknown Author';
        }
        
        if (layoutData.images && Array.isArray(layoutData.images)) {
          const imageUrls = [];
          for (const img of layoutData.images) {
            if (img.url_list && img.url_list.length > 0) {
              imageUrls.push(img.url_list[0]);
            } else if (img.url) {
              imageUrls.push(img.url);
            }
          }
          noteInfo.image_urls = imageUrls;
          noteInfo.cover_url = imageUrls.length > 0 ? imageUrls[0] : '';
          noteInfo.image_count = imageUrls.length;
        }
        
        logger.debug(`从layoutData直接提取到图片集信息: title=${noteInfo.title}, image_count=${noteInfo.image_count}`);
        return noteInfo;
      }
      
      logger.debug('无法从note_layout提取图片集信息');
      return null;
      
    } catch (error) {
      logger.error(`从note_layout提取图片集信息失败: ${error.message}`);
      return null;
    }
  }

  /**
   * 从HTML内容中解析图片集基本信息
   * @param {string} htmlContent 页面HTML内容
   * @returns {Object} 图片集信息字典
   */
  parseNoteHtmlContent(htmlContent) {
    try {
      const $ = cheerio.load(htmlContent);
      const noteInfo = {};
      
      // 尝试从标题标签获取
      const titleTag = $('title').first();
      if (titleTag.length > 0) {
        let title = titleTag.text();
        // 清理标题，移除"抖音"等后缀
        title = title.replace(/[-_|]\s*抖音.*$/, '').trim();
        noteInfo.title = cleanText(title);
      }
      
      // 尝试从meta标签获取描述
      const descTag = $('meta[name="description"]').first();
      if (descTag.length > 0) {
        noteInfo.description = cleanText(descTag.attr('content') || '');
      }
      
      // 尝试获取图片URL
      const imageTags = $('img');
      const imageUrls = [];
      imageTags.each((i, elem) => {
        const src = $(elem).attr('src') || $(elem).attr('data-src');
        if (src && src.includes('douyinpic.com')) {
          imageUrls.push(src);
        }
      });
      
      noteInfo.image_urls = imageUrls;
      
      // 尝试获取封面图
      const coverTag = $('meta[property="og:image"]').first();
      if (coverTag.length > 0) {
        noteInfo.cover_url = coverTag.attr('content');
      }
      
      // 设置默认值
      noteInfo.title = noteInfo.title || 'Unknown Title';
      noteInfo.description = noteInfo.description || '';
      noteInfo.author = noteInfo.author || 'Unknown Author';
      noteInfo.video_url = null; // 图片集没有视频URL
      noteInfo.cover_url = noteInfo.cover_url || (imageUrls.length > 0 ? imageUrls[0] : null);
      noteInfo.duration = 0;
      noteInfo.create_time = noteInfo.create_time || null;
      noteInfo.video_id = noteInfo.video_id || '';
      noteInfo.image_count = imageUrls.length;
      
      logger.debug(`HTML图片集解析结果: title=${noteInfo.title}, image_count=${noteInfo.image_count}`);
      return noteInfo;
      
    } catch (error) {
      logger.error(`解析图片集HTML内容失败: ${error.message}`);
      return null;
    }
  }
}

/**
 * 主函数 - 处理抖音链接并返回下载直链
 * @param {string} input 输入的抖音链接或包含抖音链接的文本
 * @returns {Promise<Object|null>} 视频信息对象
 */
async function getDouyinDownloadUrl(input) {
  try {
    // 提取抖音链接
    const links = extractDouyinLinks(input);
    
    if (links.length === 0) {
      // 如果没有提取到链接，检查输入本身是否是链接
      if (isValidDouyinLink(input)) {
        links.push(input);
      } else {
        logger.error('未找到有效的抖音链接');
        return null;
      }
    }
    
    // 使用第一个有效链接
    const shareUrl = links[0];
    logger.info(`处理抖音链接: ${shareUrl}`);
    
    // 创建视频提取器实例
    const extractor = new VideoExtractor();
    
    // 提取视频信息
    const videoInfo = await extractor.extractVideoInfo(shareUrl);
    
    if (videoInfo) {
      logger.info('成功提取视频信息');
      return videoInfo;
    } else {
      logger.error('提取视频信息失败');
      return null;
    }
    
  } catch (error) {
    logger.error(`处理失败: ${error.message}`);
    return null;
  }
}

/**
 * 命令行使用
 */
async function main() {
  // 获取命令行参数
  const args = process.argv.slice(2);
  
  if (args.length === 0) {
    console.log('使用方法:');
    console.log('  node douyin_downloader_single.js "https://v.douyin.com/ww8JpyDgr1o/"');
    console.log('  node douyin_downloader_single.js "1.28 【推荐你看】复制打开Dǒu音👀忽见粉蝶潜入窗～ # 凉夜横塘 # 小歆摇 # 翻... https://v.douyin.com/ww8JpyDgr1o/ j@P.kc VYz:/ 08/08"');
    process.exit(1);
  }
  
  const input = args.join(' ');
  
  console.log('='.repeat(60));
  console.log('抖音视频直链获取工具 - 单文件版本');
  console.log('='.repeat(60));
  console.log(`开始时间: ${new Date().toLocaleString()}`);
  console.log();
  
  // 处理链接
  const result = await getDouyinDownloadUrl(input);
  
  if (result) {
    console.log('提取成功!');
    console.log('-'.repeat(40));
    console.log(`标题: ${result.title}`);
    console.log(`作者: ${result.author}`);
    console.log(`类型: ${result.content_type === 'video' ? '视频' : '图片集'}`);
    
    if (result.content_type === 'video') {
      console.log(`视频链接: ${result.video_url}`);
      console.log(`时长: ${result.duration}秒`);
    } else {
      console.log(`图片数量: ${result.image_count}`);
      console.log(`图片链接:`);
      result.image_urls.forEach((url, index) => {
        console.log(`  ${index + 1}. ${url}`);
      });
    }
    
    console.log(`封面: ${result.cover_url}`);
    console.log(`点赞: ${result.like_count}`);
    console.log(`评论: ${result.comment_count}`);
    console.log(`分享: ${result.share_count}`);
    console.log('-'.repeat(40));
    console.log(`完成时间: ${new Date().toLocaleString()}`);
    console.log('='.repeat(60));
    
    // 输出JSON格式（可选）
    if (process.env.JSON_OUTPUT) {
      console.log('\nJSON格式输出:');
      console.log(JSON.stringify(result, null, 2));
    }
  } else {
    console.error('提取失败!');
    process.exit(1);
  }
}

// 导出函数供其他模块使用
module.exports = {
  getDouyinDownloadUrl,
  extractDouyinLinks,
  isValidDouyinLink,
  VideoExtractor,
  HttpClient
};

// 如果直接运行此文件，则执行主函数
if (require.main === module) {
  main().catch(error => {
    console.error('未捕获的错误:', error);
    process.exit(1);
  });
}
