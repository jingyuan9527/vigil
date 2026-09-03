// Bento 卡片容器：强制 rounded-2xl + 边框 + 悬停上浮，支持跨网格尺寸。
export default function BentoCard({ className = '', span = '', children, as: Tag = 'div', ...rest }) {
  const spanClass = span === 'lg' ? 'bento-lg' : span === 'wide' ? 'bento-wide' : span === 'tall' ? 'bento-tall' : ''
  return (
    <Tag className={`bento-card p-4 md:p-6 ${spanClass} ${className}`} {...rest}>
      {children}
    </Tag>
  )
}
