function BookCoverStore() {
  if (!(this instanceof BookCoverStore)) return new BookCoverStore()

  riot.observable(this)

  var self = this

  self.on('ve_bookcover_list_init', function() {
    console.log('enter init')
    self.trigger("ve_bookcover_get_data", "/index.js")
  })

  self.on('ve_bookcover_get_data', function(data_url) {
    console.log("get data " + data_url)
    $.ajax({
      dataType: "json",
      url: data_url,
      success: function(data) {
        self.bookcovers = data
        self.trigger('se_bookcovers_changed', self.bookcovers)
      },
    })
  })
}
