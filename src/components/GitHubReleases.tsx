'use client';

import { useEffect, useState } from 'react';

interface Asset {
  name: string;
  browser_download_url: string;
  size: number;
}

interface Release {
  name: string;
  tag_name: string;
  published_at: string;
  body: string;
  html_url: string;
  assets: Asset[];
}

export function GitHubReleases() {
  const [release, setRelease] = useState<Release | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchLatestRelease() {
      try {
        const response = await fetch(
          'https://api.github.com/repos/Dogebox-WG/os/releases/latest'
        );
        
        if (!response.ok) {
          throw new Error(`Failed to fetch release: ${response.statusText}`);
        }
        
        const data = await response.json();
        setRelease(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch release');
      } finally {
        setLoading(false);
      }
    }

    fetchLatestRelease();
  }, []);

  if (loading) {
    return (
      <div className="border border-gray-200 rounded-lg p-6 animate-pulse">
        <div className="h-8 bg-gray-200 rounded w-1/3 mb-4"></div>
        <div className="h-4 bg-gray-200 rounded w-1/4 mb-6"></div>
        <div className="space-y-3">
          <div className="h-4 bg-gray-200 rounded"></div>
          <div className="h-4 bg-gray-200 rounded"></div>
          <div className="h-4 bg-gray-200 rounded w-5/6"></div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="border border-red-200 bg-red-50 rounded-lg p-6">
        <p className="text-red-600">Error loading release: {error}</p>
      </div>
    );
  }

  if (!release) {
    return null;
  }

  const formatFileSize = (bytes: number) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  const getDaysAgo = (dateString: string) => {
    const releaseDate = new Date(dateString);
    const currentDate = new Date();
    const diffTime = Math.abs(currentDate.getTime() - releaseDate.getTime());
    const diffDays = Math.floor(diffTime / (1000 * 60 * 60 * 24));
    return diffDays;
  };

  // Function to get appropriate icon for file type
  const getFileIcon = (fileName: string) => {
    const extension = fileName.toLowerCase().split('.').pop();
    switch (extension) {
      case 'iso':
        return '💿';
      case 'zip':
      case 'tar':
      case 'gz':
      case 'bz2':
      case '7z':
        return '📦';
      case 'exe':
      case 'msi':
        return '⚙️';
      case 'deb':
      case 'rpm':
        return '📦';
      case 'dmg':
        return '💽';
      case 'txt':
      case 'md':
        return '📄';
      case 'sig':
      case 'asc':
        return '🔐';
      default:
        return '📁';
    }
  };

    // Simple markdown-to-HTML parser for GitHub release notes
  const parseMarkdown = (text: string) => {
    // Process GitHub-style alerts using line-by-line approach
    const processAlerts = (content: string): string => {
      const lines = content.split('\n');
      const result: string[] = [];
      let i = 0;

      while (i < lines.length) {
        const line = lines[i];
        const alertMatch = line.match(/^> \[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*$/);

        if (alertMatch) {
          const alertType = alertMatch[1];
          const alertContentLines: string[] = [];
          i++; // Move past the alert header

          // Collect all subsequent lines that start with >
          while (i < lines.length && lines[i].startsWith('>')) {
            alertContentLines.push(lines[i]);
            i++;
          }

          // Remove the > prefix from each line
          const cleanContent = alertContentLines
            .map((line: string) => line.replace(/^>\s?/, ''))
            .join('\n')
            .trim();

          // Get alert styling based on type
          const getAlertStyle = (type: string) => {
            switch (type) {
              case 'NOTE':
                return { container: 'border-blue-200 bg-blue-50', icon: '📝', iconColor: 'text-blue-600', title: 'text-blue-800' };
              case 'TIP':
                return { container: 'border-green-200 bg-green-50', icon: '💡', iconColor: 'text-green-600', title: 'text-green-800' };
              case 'IMPORTANT':
                return { container: 'border-purple-200 bg-purple-50', icon: '❗', iconColor: 'text-purple-600', title: 'text-purple-800' };
              case 'WARNING':
                return { container: 'border-yellow-200 bg-yellow-50', icon: '⚠️', iconColor: 'text-yellow-600', title: 'text-yellow-800' };
              case 'CAUTION':
                return { container: 'border-red-200 bg-red-50', icon: '🚨', iconColor: 'text-red-600', title: 'text-red-800' };
              default:
                return { container: 'border-gray-200 bg-gray-50', icon: 'ℹ️', iconColor: 'text-gray-600', title: 'text-gray-800' };
            }
          };

          const style = getAlertStyle(alertType);
          
          result.push(`<div class="border ${style.container} rounded-lg p-4 my-4">
            <div class="flex items-center gap-2 mb-2">
              <span class="${style.iconColor}">${style.icon}</span>
              <span class="font-semibold ${style.title}">${alertType}</span>
            </div>
            <div class="text-sm">ALERT_CONTENT_${cleanContent}_ALERT_CONTENT</div>
          </div>`);
          
          // Don't increment i here as it's already at the next non-alert line
        } else {
          result.push(line);
          i++;
        }
      }

      return result.join('\n');
    };

    const processedText = processAlerts(text);

    // Now process regular markdown
    return processedText
      // Convert headers
      .replace(/^### (.*$)/gm, '<h3 class="text-lg font-semibold mt-4 mb-2">$1</h3>')
      .replace(/^## (.*$)/gm, '<h2 class="text-xl font-bold mt-6 mb-3">$1</h2>')
      .replace(/^# (.*$)/gm, '<h1 class="text-2xl font-bold mt-6 mb-4">$1</h1>')
      // Convert bold text
      .replace(/\*\*(.*?)\*\*/g, '<strong class="font-semibold">$1</strong>')
      // Convert italic text
      .replace(/\*(.*?)\*/g, '<em class="italic">$1</em>')
      // Convert code blocks
      .replace(/```([\s\S]*?)```/g, '<pre class="bg-gray-100 p-3 rounded text-sm overflow-x-auto my-2"><code>$1</code></pre>')
      // Convert inline code
      .replace(/`([^`]+)`/g, '<code class="bg-gray-100 px-1 py-0.5 rounded text-sm">$1</code>')
      // Convert markdown links first
      .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">$1</a>')
      // Convert plain URLs to links (avoiding already linked URLs)
      .replace(/(^|[^">])(https?:\/\/[^\s<>"']+)/g, '$1<a href="$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">$2</a>')
      // Convert GitHub usernames (@username) to links
      .replace(/(^|[^a-zA-Z0-9])@([a-zA-Z0-9](?:[a-zA-Z0-9]|-(?=[a-zA-Z0-9])){0,38})/g, '$1<a href="https://github.com/$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">@$2</a>')
      // Convert bullet points
      .replace(/^[\s]*[-*+][\s]+(.*$)/gm, '<li class="ml-4">$1</li>')
      // Wrap consecutive list items in ul tags
      .replace(/(<li.*<\/li>\s*)+/g, '<ul class="list-disc list-inside my-2 space-y-1">$&</ul>')
      // Process alert content recursively
      .replace(/ALERT_CONTENT_([\s\S]*?)_ALERT_CONTENT/g, (match, content) => {
        return content
          // Convert headers
          .replace(/^### (.*$)/gm, '<h3 class="text-lg font-semibold mt-2 mb-1">$1</h3>')
          .replace(/^## (.*$)/gm, '<h2 class="text-xl font-bold mt-2 mb-1">$1</h2>')
          .replace(/^# (.*$)/gm, '<h1 class="text-2xl font-bold mt-2 mb-1">$1</h1>')
          // Convert bold text
          .replace(/\*\*(.*?)\*\*/g, '<strong class="font-semibold">$1</strong>')
          // Convert italic text
          .replace(/\*(.*?)\*/g, '<em class="italic">$1</em>')
          // Convert links
          .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">$1</a>')
          // Convert plain URLs
          .replace(/(^|[^">])(https?:\/\/[^\s<>"']+)/g, '$1<a href="$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">$2</a>')
          // Convert GitHub usernames (@username) to links
          .replace(/(^|[^a-zA-Z0-9])@([a-zA-Z0-9](?:[a-zA-Z0-9]|-(?=[a-zA-Z0-9])){0,38})/g, '$1<a href="https://github.com/$2" class="text-blue-600 hover:text-blue-800 underline" target="_blank" rel="noopener noreferrer">@$2</a>')
          // Convert simple line breaks to paragraphs
          .split('\n\n')
          .map((paragraph: string) => paragraph.trim())
          .filter((paragraph: string) => paragraph.length > 0)
                     .map((paragraph: string) => {
             if (paragraph.match(/^<(h[1-6])/)) {
               return paragraph;
             }
             return `<p class="mb-2">${paragraph}</p>`;
           })
          .join('');
      })
      // Convert line breaks to paragraphs
      .split('\n\n')
      .map((paragraph: string) => paragraph.trim())
      .filter((paragraph: string) => paragraph.length > 0)
      .map((paragraph: string) => {
        // Don't wrap if already wrapped in HTML tags
        if (paragraph.match(/^<(h[1-6]|ul|pre|div)/)) {
          return paragraph;
        }
        return `<p class="mb-3">${paragraph}</p>`;
      })
      .join('');
  };

  return (
    <div className="border border-gray-200 rounded-lg p-6 mb-6 bg-white shadow-sm">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h3 className="text-2xl font-bold flex items-center gap-2">
            {release.name || release.tag_name}
            <span className="text-gray-500 text-lg">🏷️</span>
          </h3>
          <p className="text-gray-600 flex items-center gap-2 mt-1">
            <span>📅</span>
            Released on {formatDate(release.published_at)}
            {getDaysAgo(release.published_at) > 0 && (
              <span className="text-gray-500 text-sm"> ({getDaysAgo(release.published_at)} days ago)</span>
            )}
          </p>
        </div>
        <a
          href={release.html_url}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-2 text-gray-600 hover:text-gray-800 transition-colors no-underline"
          style={{ textDecoration: 'none' }}
        >
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
          </svg>
          View on GitHub
        </a>
      </div>

      {release.body && (
        <div className="mb-6">
          <h4 className="text-lg font-semibold mb-3">Release Notes</h4>
          <div 
            className="prose prose-sm max-w-none text-gray-700"
            dangerouslySetInnerHTML={{ __html: parseMarkdown(release.body) }}
          />
        </div>
      )}

      <div className="mt-6">
        <h4 className="text-lg font-semibold mb-3 flex items-center gap-2">
          <span>💾</span>
          Available Downloads
        </h4>
        
        <div className="grid gap-3">
          {release.assets.map((asset) => (
            <div
              key={asset.name}
              className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors group"
            >
              <div className="flex items-center gap-3">
                <span className="text-2xl group-hover:scale-110 transition-transform">
                  {getFileIcon(asset.name)}
                </span>
                <div>
                  <p className="font-medium text-gray-900">{asset.name}</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-sm text-gray-500">{formatFileSize(asset.size)}</span>
                <a
                  href={asset.browser_download_url}
                  download
                  className="inline-flex items-center gap-1 px-4 py-2 bg-blue-600 text-white text-sm font-medium rounded-md hover:bg-blue-700 transition-colors no-underline"
                  style={{ textDecoration: 'none' }}
                >
                  Download
                  <span>→</span>
                </a>
              </div>
            </div>
          ))}
        </div>
        
        {release.assets.length === 0 && (
          <p className="text-gray-500 italic">No downloads available for this release.</p>
        )}
      </div>
    </div>
  );
} 