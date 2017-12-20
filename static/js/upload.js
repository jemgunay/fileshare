var maxAutoCompleteSuggestions = 5;

$(document).ready(function() {
    // send message on button click
    $("form#upload-form").submit(function(e) {
        e.preventDefault();
        var formData = new FormData(this);
        performUploadRequest(hostname + "/upload", "POST", formData, function(html) {
            // TODO
            window.location = "/"
        });
    });
});