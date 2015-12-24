require 'active_record'
require 'sqlite3'
require 'ruby-epub'
require 'fileutils'
require 'composite_primary_keys'

class Book < ActiveRecord::Base
  self.primary_key = :id

  has_many :authors, through: :book_authors
  has_many :tags, through: :book_tags
  has_many :book_authors, :foreign_key => [:author_id, :book_id]
  has_many :book_tags, :foreign_key => [:tag_id, :book_id]
end

class Author < ActiveRecord::Base
  self.primary_key = :id

  has_many :books, through: :book_authors
end

class Tag < ActiveRecord::Base
  self.primary_key = :id

  has_many :books, through: :book_tags
end

class BookAuthor < ActiveRecord::Base
  self.primary_keys = :book_id, :author_id

  belongs_to :author
  belongs_to :book
end

class BookTag < ActiveRecord::Base
  self.primary_keys = :tag_id, :book_id

  belongs_to :tag
  belongs_to :book
end

ActiveRecord::Base.establish_connection(adapter: 'sqlite3', database: 'gopds.db')

Dir.new(ARGV[0]).each do |f|
  begin
    if f.match(".epub")
      puts f
      epub_meta = Epub.new(ARGV[0] + "/" + f)
      book = Book.new
      author = nil
      if epub_meta.respond_to?('creator')
        author = Author.where(name: epub_meta.creator).first
        if author.nil?
          author = Author.new
          author.name = epub_meta.creator
          author.save
        end
      end
      if epub_meta.respond_to?('title')
        book.title = epub_meta.title
      end
      if epub_meta.respond_to?('description')
        book.description = epub_meta.description
      end
      # if epub_meta.respond_to?('calibre_series_index')
      #   self.serie_number = epub_meta.calibre_series_index.to_i
      # end
      # if epub_meta.respond_to?('calibre_series')
      #   self.serie = epub_meta.calibre_series
      # end
      if epub_meta.respond_to?('language')
        book.language = epub_meta.language
      end
      if epub_meta.respond_to?('subject')
        tag = Tag.where(name: epub_meta.subject).first
        if tag.nil?
          tag = Tag.new
          tag.name = epub_meta.subject
          tag.save
        end
        if tag
         book.tags = [tag]
       end
      end
      book.save
      unless author.nil?
        book_author = BookAuthor.new
        book_author.author_id = author.id
        book_author.book_id = book.id
        book_author.save
      end
      if(epub_meta.cover_image && epub_meta.cover_image.size)
        puts "file path " + epub_meta.cover_image.path
        FileUtils.mkdir("public/books/#{book.id}")
        FileUtils.cp(epub_meta.cover_image.path, "public/books/#{book.id}/#{book.id}.jpg")
        book.cover_path = "public/books/#{book.id}/#{book.id}.jpg"
        book.cover_type = "image/jpeg"
        book.save
      end
      FileUtils.mv(ARGV[0] + "/" + f, "public/books/#{book.id}/#{book.id}.epub")
      puts book.inspect
    end
  rescue => e
    puts e
    puts e.backtrace
  end
end
